package service

import (
	"log"
	"os"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/model"
)

// seedChannelSpec 描述一个可以通过环境变量预置的系统渠道。
// 只要对应的 APIKeyEnv 未设置，就完全跳过——不影响不需要这个功能的部署。
type seedChannelSpec struct {
	Name           string                     // 渠道名称，用于判重（已存在同名渠道则跳过，不会覆盖管理员的手工修改）
	APIKeyEnv      string                     // 必填：从该环境变量读取 API Key
	BaseURLEnv     string                     // 可选：从该环境变量覆盖默认 Base URL
	DefaultBaseURL string                     // 默认 Base URL
	InterfaceType  model.ChannelInterfaceType // 渠道接口类型（决定按 chat / image / video 中的哪种协议转发）
	ModelsEnv      string                     // 可选：从该环境变量读取逗号分隔的模型列表，覆盖默认模型
	DefaultModels  []string                   // 默认模型列表
}

// seedChannelSpecs 目前预置 Gemini（文本 + 图片）与 xAI Grok（视频）三个渠道。
// Gemini 的文本、图片走 Google 官方 OpenAI 兼容层，同一个 Base URL 但要分成两个渠道，
// 因为本项目一个渠道只对应一种接口类型（chat-completion / openai-image / xai-video ...）。
var seedChannelSpecs = []seedChannelSpec{
	{
		Name:           "Gemini 文本",
		APIKeyEnv:      "CANVAS_SEED_GEMINI_API_KEY",
		BaseURLEnv:     "CANVAS_SEED_GEMINI_BASE_URL",
		DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/",
		InterfaceType:  model.ChannelInterfaceChatCompletion,
		ModelsEnv:      "CANVAS_SEED_GEMINI_TEXT_MODELS",
		DefaultModels:  []string{"gemini-3.5-flash"},
	},
	{
		Name:           "Gemini 图片",
		APIKeyEnv:      "CANVAS_SEED_GEMINI_API_KEY",
		BaseURLEnv:     "CANVAS_SEED_GEMINI_BASE_URL",
		DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta/openai/",
		InterfaceType:  model.ChannelInterfaceOpenAIImage,
		ModelsEnv:      "CANVAS_SEED_GEMINI_IMAGE_MODELS",
		DefaultModels:  []string{"gemini-3-pro-image-preview", "gemini-2.5-flash-image"},
	},
	{
		Name:           "xAI Grok 视频",
		APIKeyEnv:      "CANVAS_SEED_XAI_API_KEY",
		BaseURLEnv:     "CANVAS_SEED_XAI_BASE_URL",
		DefaultBaseURL: "https://api.x.ai/v1",
		InterfaceType:  model.ChannelInterfaceXAIVideo,
		ModelsEnv:      "CANVAS_SEED_XAI_VIDEO_MODELS",
		DefaultModels:  []string{"grok-video-1.5", "grok-video-1.5-1080p"},
	},
}

// EnsureSeedSystemChannels 在服务启动时按环境变量预置系统渠道，用于个人 fork 免登录后台点击配置。
//
// 使用方式：部署时设置
//
//	CANVAS_SEED_GEMINI_API_KEY=xxx   # 同时开通 “Gemini 文本” 和 “Gemini 图片” 两个渠道
//	CANVAS_SEED_XAI_API_KEY=xxx      # 开通 “xAI Grok 视频” 渠道
//
// 未设置对应 *_API_KEY 的渠道会被跳过；已存在同名渠道也会跳过，不会覆盖后台的手工修改，
// 所以这个函数在每次启动时调用都是安全的（幂等）。
//
// 预置的模型默认按 0 积分（自用免费）标记为“已定价”，避免因为没有在后台配置价格而被计费流程拦截；
// 如果你要把这个 fork 开放给其他用户使用并按次数收费，记得去后台“渠道管理”里重新设置真实价格。
func (s *Service) EnsureSeedSystemChannels() error {
	existing, err := s.repo.SystemChannels(true)
	if err != nil {
		return err
	}
	existingNames := make(map[string]bool, len(existing))
	for _, ch := range existing {
		existingNames[ch.Name] = true
	}

	defaultPriceMicrocredits := envInt64("CANVAS_SEED_DEFAULT_PRICE_MICROCREDITS", 0)

	for _, spec := range seedChannelSpecs {
		apiKey := strings.TrimSpace(os.Getenv(spec.APIKeyEnv))
		if apiKey == "" {
			continue
		}
		if existingNames[spec.Name] {
			continue
		}

		baseURL := strings.TrimSpace(os.Getenv(spec.BaseURLEnv))
		if baseURL == "" {
			baseURL = spec.DefaultBaseURL
		}

		models := spec.DefaultModels
		if raw := strings.TrimSpace(os.Getenv(spec.ModelsEnv)); raw != "" {
			models = splitAndTrimNonEmpty(raw, ",")
		}

		req := ChannelRequest{
			Name:          spec.Name,
			BaseURL:       baseURL,
			APIKey:        apiKey,
			InterfaceType: string(spec.InterfaceType),
			Models:        models,
		}

		channel, err := channelFromRequest(req, model.ModelChannel{
			ID:      newID(),
			UserID:  "system-seed",
			Scope:   model.ChannelScopeSystem,
			Enabled: true,
		})
		if err != nil {
			// 配置错误（比如 Base URL 校验不过）不应该导致整个服务起不来，打印日志后跳过即可。
			log.Printf("seed channel %q skipped: %v", spec.Name, err)
			continue
		}
		if err := s.repo.Create(&channel); err != nil {
			return err
		}
		if err := s.syncInitialChannelModels(&channel, models); err != nil {
			return err
		}
		if err := s.markSeedChannelModelsPriced(channel.ID, defaultPriceMicrocredits); err != nil {
			return err
		}
		log.Printf("seed channel %q created with %d model(s)", spec.Name, len(models))
	}
	return nil
}

// markSeedChannelModelsPriced 把预置渠道下新建的模型标记为“已定价”，价格默认为 0（自用免费）。
// 不会改动已经有定价的模型，避免和后续手工调价冲突。
func (s *Service) markSeedChannelModelsPriced(channelID string, unitPriceMicrocredits int64) error {
	items, err := s.repo.ChannelModels(channelID, true)
	if err != nil {
		return err
	}
	for index := range items {
		if items[index].PriceConfigured {
			continue
		}
		items[index].PriceConfigured = true
		items[index].UnitPriceMicrocredits = unitPriceMicrocredits
		items[index].BillingMode = "fixed_request"
		items[index].PriceVersion++
		if err := s.repo.SaveChannelModel(&items[index]); err != nil {
			return err
		}
	}
	return nil
}

func splitAndTrimNonEmpty(raw string, sep string) []string {
	parts := strings.Split(raw, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func envInt64(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}
