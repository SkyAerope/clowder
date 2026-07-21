package app

import (
	"context"
	"encoding/json"
	"math/rand"
	"time"

	"github.com/SkyAerope/clowder/internal/script"
	"github.com/SkyAerope/clowder/internal/store"
)

type CheckProbeResult struct {
	Status        string
	ModelResponse string
	TTFTMS        int64
	StatusCode    int
	ResponseBody  string
}

// CheckKey 对单个 key 跑 check；chain=true 时结果回灌引擎。
// 优先级：scripts.check > default_check_model 推理探活 > 随机 healthy 模型探活。
func (a *App) CheckKey(ctx context.Context, keyID int64, chain bool) (string, error) {
	result, err := a.checkKey(ctx, keyID, chain)
	return result.Status, err
}

func (a *App) CheckKeyWithProbe(ctx context.Context, keyID int64, chain bool) (CheckProbeResult, error) {
	return a.checkKey(ctx, keyID, chain)
}

func (a *App) checkKey(ctx context.Context, keyID int64, chain bool) (CheckProbeResult, error) {
	k := a.Pool.FindByID(keyID)
	if k == nil {
		return CheckProbeResult{}, nil
	}
	ps := a.ConfiguredProvider(k.Provider)
	if ps == nil {
		return CheckProbeResult{}, nil
	}
	m := ps.Manifest

	var (
		res *script.CheckResult
		err error
	)
	switch {
	case m.Scripts.Check != "":
		res, err = a.Runner.Check(ctx, m.Dir, m.Scripts.Check, k.APIKey,
			time.Duration(m.Scripts.CheckTimeout)*time.Second)
	default:
		model := m.DefaultCheckModel
		if model == "" {
			model = a.pickRandomHealthyModel(k.Provider)
		}
		if model == "" {
			a.Log.Warn("check key: no check script and no healthy model for probe", "key", keyID, "provider", k.Provider)
			return CheckProbeResult{}, nil
		}
		probe, probeErr := a.checkViaChat(ctx, m.BaseURL, k.APIKey, model,
			time.Duration(m.Scripts.CheckTimeout)*time.Second)
		if probeErr != nil {
			err = probeErr
		} else {
			res = &script.CheckResult{Status: probe.Status}
		}
		if err == nil {
			if res.Metadata != nil {
				if b, marshalErr := json.Marshal(res.Metadata); marshalErr == nil {
					_ = a.Pool.UpdateKeyMetadata(keyID, string(b))
				}
			}
			a.Log.Info("check key result", "key", keyID, "provider", k.Provider, "status", res.Status)
			if chain {
				a.FeedCheckResult(k.Provider, keyID, res.Status)
			}
			return CheckProbeResult{Status: probe.Status, ModelResponse: probe.ModelResponse, TTFTMS: probe.TTFTMS}, nil
		}
	}
	if err != nil {
		a.Log.Warn("check key", "key", keyID, "error", err)
		return CheckProbeResult{}, err
	}
	if res.Metadata != nil {
		if b, err := json.Marshal(res.Metadata); err == nil {
			_ = a.Pool.UpdateKeyMetadata(keyID, string(b))
		}
	}
	a.Log.Info("check key result", "key", keyID, "provider", k.Provider, "status", res.Status)
	if chain {
		a.FeedCheckResult(k.Provider, keyID, res.Status)
	}
	return CheckProbeResult{
		Status:        res.Status,
		ModelResponse: res.ModelResponse,
		TTFTMS:        res.TTFTMS,
	}, nil
}

func (a *App) pickRandomHealthyModel(providerName string) string {
	ps := a.Provider(providerName)
	if ps == nil {
		return ""
	}
	var natives []string
	for _, native := range ps.Models {
		if a.ModelRoutable(providerName, native) {
			natives = append(natives, native)
		}
	}
	if len(natives) == 0 {
		return ""
	}
	return natives[rand.Intn(len(natives))]
}

// CheckKeys 对某 provider（空 = 全部）的所有 key 逐个跑 check。
func (a *App) CheckKeys(ctx context.Context, providerName string, chain bool) error {
	var provs []string
	if providerName != "" {
		provs = []string{providerName}
	} else {
		for name := range a.Providers() {
			provs = append(provs, name)
		}
	}
	for _, p := range provs {
		for _, k := range a.Pool.Keys(p) {
			if k.Status == store.KeyInvalid {
				continue
			}
			a.CheckKey(ctx, k.ID, chain)
		}
	}
	return nil
}
