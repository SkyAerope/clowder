package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DiscoverModels 从 provider 拉取可选模型，不修改运行时或 manifest 配置。
func (a *App) DiscoverModels(ctx context.Context, name string) ([]string, error) {
	ps := a.ConfiguredProvider(name)
	if ps == nil {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return fetchModelList(ctx, ps.Manifest.BaseURL, a.firstKey(name))
}

// firstKey 取该 provider 任一可用 key 用于拉模型列表（无 key 返回空 = 匿名请求）。
func (a *App) firstKey(name string) string {
	k, err := a.Pool.Pick(name, nil)
	if err != nil {
		return ""
	}
	return k.APIKey
}

// fetchModelList GET {base}/v1/models（base 已含 /v1 时不重复拼）。
func fetchModelList(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	url := JoinURL(baseURL, "/v1/models")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET %s: %d %s", url, resp.StatusCode, string(body))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		out = append(out, d.ID)
	}
	return out, nil
}

// JoinURL 拼接 base_url 与 /v1/xxx 路径；base 已以 /v1 结尾时去重。
func JoinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") {
		path = strings.TrimPrefix(path, "/v1")
	}
	return base + path
}
