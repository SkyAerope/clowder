package app

import (
	"context"
	"fmt"
	"time"

	"github.com/SkyAerope/clowder/internal/rules"
)

type ModelCheckResult struct {
	Status        string
	KeyID         int64
	ModelResponse string
	TTFTMS        int64
	RuleSlug      string
	StatusCode    int
	ResponseBody  string
}

type ModelCheckRuleApplication struct {
	ProviderName string
	NativeModel  string
	KeyID        int64
	RuleSlug     string
	StatusCode   int
	ResponseBody string
}

func (a *App) CheckModel(ctx context.Context, providerName, nativeModel string) (ModelCheckResult, error) {
	providerState := a.ConfiguredProvider(providerName)
	if providerState == nil {
		return ModelCheckResult{}, errProviderNotFound(providerName)
	}
	if !providerHasNative(providerState, nativeModel) {
		return ModelCheckResult{}, fmt.Errorf("model %s not found for provider %s", nativeModel, providerName)
	}
	key, err := a.Pool.Pick(providerName, nil)
	if err != nil {
		return ModelCheckResult{}, fmt.Errorf("pick key for provider %s: %w", providerName, err)
	}
	probe, err := a.checkViaChat(
		ctx,
		providerState.Manifest.BaseURL,
		key.APIKey,
		nativeModel,
		time.Duration(providerState.Manifest.Scripts.CheckTimeout)*time.Second,
	)
	if err != nil {
		return ModelCheckResult{}, err
	}
	canonicalModel := a.primaryCanonical(nativeModel)
	event := &rules.Event{
		Type: "upstream", Provider: providerName, Model: canonicalModel,
		StatusCode: probe.StatusCode, Body: probe.ResponseBody,
		TTFTMs: probe.TTFTMS, IsError: probe.StatusCode < 200 || probe.StatusCode >= 300,
		AttemptNo: 1,
	}
	matched := a.Engine.Match(event)
	ruleSlug := ""
	if matched != nil && len(matched.Do) > 0 {
		ruleSlug = matched.Slug
	}
	if probe.Status == "active" {
		a.Health.MarkSuccess(providerName, nativeModel, probe.TTFTMS)
	}
	return ModelCheckResult{
		Status: probe.Status, KeyID: key.ID, ModelResponse: probe.ModelResponse,
		TTFTMS: probe.TTFTMS, RuleSlug: ruleSlug,
		StatusCode: probe.StatusCode, ResponseBody: probe.ResponseBody,
	}, nil
}

func (a *App) ApplyModelCheckRule(application ModelCheckRuleApplication) error {
	providerState := a.ConfiguredProvider(application.ProviderName)
	if providerState == nil {
		return errProviderNotFound(application.ProviderName)
	}
	if !providerHasNative(providerState, application.NativeModel) {
		return fmt.Errorf("model %s not found for provider %s", application.NativeModel, application.ProviderName)
	}
	key := a.Pool.FindByID(application.KeyID)
	if key == nil || key.Provider != application.ProviderName {
		return fmt.Errorf("key %d not found for provider %s", application.KeyID, application.ProviderName)
	}
	event := &rules.Event{
		Type: "upstream", Provider: application.ProviderName, Model: a.primaryCanonical(application.NativeModel),
		StatusCode: application.StatusCode, Body: application.ResponseBody,
		IsError: application.StatusCode < 200 || application.StatusCode >= 300, AttemptNo: 1,
	}
	matched := a.Engine.Match(event)
	if matched == nil || matched.Slug != application.RuleSlug || len(matched.Do) == 0 {
		return fmt.Errorf("applicable rule %s not found", application.RuleSlug)
	}
	a.RunActions(&ActionContext{
		Rule: matched, Event: event, KeyID: application.KeyID,
		Provider: application.ProviderName, Model: event.Model, Native: application.NativeModel,
		RespBody: application.ResponseBody, StatusCode: application.StatusCode,
	})
	return nil
}
