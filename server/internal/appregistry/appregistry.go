// Package appregistry 消费 app-registry/v1 静态应用清单(HUI-1724 契约,
// public-ai docs/contracts/app-registry/v1/README.md)。本应用侧只做三件事:
// 严格加载校验、按 (app, target_id, kind) 精确解析 target URL、按 app 查
// receipt/launch target。加载即校验,失败拒绝;URL 白名单:精确 https 或仅
// loopback 允许 http;禁止 query/fragment/userinfo,path 必填——登记表里
// 永远没有可注入参数或开放重定向的目的地。
package appregistry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

// ManifestVersion 唯一支持的清单版本(版本化:其他版本显式拒绝)。
const ManifestVersion = "app-registry/v1"

// MaxManifestBytes 清单 1 MiB 上限。
const MaxManifestBytes = 1 << 20

// TargetKind launch / receipt(与契约一致;事件接收端不在本包职责)。
const (
	KindLaunch  = "launch"
	KindReceipt = "receipt"
)

// ErrUnknownManifestVersion 版本不识别。
var ErrUnknownManifestVersion = errors.New("appregistry: unknown manifest_version")

var (
	slugRe          = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	capNameRe       = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9]+)+$`)
	sourceKindValid = map[string]bool{"order": true, "standalone": true, "campaign": true}
	targetKindValid = map[string]bool{"launch": true, "receipt": true}
)

type Capability struct {
	Name            string `json:"name"`
	MenuVisible     bool   `json:"menu_visible"`
	RequiresBilling bool   `json:"requires_billing"`
}

type Target struct {
	TargetID string `json:"target_id"`
	Kind     string `json:"kind"`
	URL      string `json:"url"`
}

type App struct {
	AppID                string       `json:"app_id"`
	DisplayName          string       `json:"display_name"`
	Enabled              bool         `json:"enabled"`
	SupportedSourceKinds []string     `json:"supported_source_kinds"`
	Capabilities         []Capability `json:"capabilities"`
	LaunchTargets        []Target     `json:"launch_targets"`
	ReceiptTargets       []Target     `json:"receipt_targets"`
}

type Manifest struct {
	ManifestVersion string `json:"manifest_version"`
	Apps            []App  `json:"apps"`
}

// LoadManifest 严格加载+校验(重复键/未知字段/尾随内容/全属性必填/URL 白名单)。
func LoadManifest(raw []byte) (*Manifest, error) {
	if len(raw) == 0 {
		return nil, errors.New("appregistry: empty manifest")
	}
	if len(raw) > MaxManifestBytes {
		return nil, fmt.Errorf("appregistry: manifest exceeds %d bytes", MaxManifestBytes)
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("appregistry: decode manifest: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("appregistry: trailing content after manifest")
	}
	if m.ManifestVersion != ManifestVersion {
		return nil, fmt.Errorf("%w: %q", ErrUnknownManifestVersion, m.ManifestVersion)
	}
	if len(m.Apps) == 0 {
		return nil, errors.New("appregistry: manifest 无应用")
	}
	seenApp := map[string]bool{}
	seenTarget := map[string]bool{}
	for i := range m.Apps {
		if err := validateApp(&m.Apps[i], seenApp, seenTarget); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

func rejectDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := kt.(string)
				if !ok {
					return errors.New("appregistry: 对象键非字符串")
				}
				if seen[key] {
					return fmt.Errorf("appregistry: 重复键 %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("appregistry: trailing content")
	}
	return nil
}

func validateApp(a *App, seenApp, seenTarget map[string]bool) error {
	if a.AppID == "" || !slugRe.MatchString(a.AppID) {
		return fmt.Errorf("appregistry: app_id 非法 %q", a.AppID)
	}
	if seenApp[a.AppID] {
		return fmt.Errorf("appregistry: app_id 重复 %q", a.AppID)
	}
	seenApp[a.AppID] = true
	if strings.TrimSpace(a.DisplayName) == "" {
		return fmt.Errorf("appregistry: app %s display_name 必填", a.AppID)
	}
	if len(a.SupportedSourceKinds) < 1 || len(a.SupportedSourceKinds) > 3 {
		return fmt.Errorf("appregistry: app %s supported_source_kinds 须 1..3", a.AppID)
	}
	seenKind := map[string]bool{}
	for _, k := range a.SupportedSourceKinds {
		if !sourceKindValid[k] {
			return fmt.Errorf("appregistry: app %s source_kind 非法 %q", a.AppID, k)
		}
		if seenKind[k] {
			return fmt.Errorf("appregistry: app %s source_kind 重复 %q", a.AppID, k)
		}
		seenKind[k] = true
	}
	if len(a.Capabilities) < 1 {
		return fmt.Errorf("appregistry: app %s capabilities 至少 1 项", a.AppID)
	}
	seenCap := map[string]bool{}
	for _, c := range a.Capabilities {
		if !capNameRe.MatchString(c.Name) {
			return fmt.Errorf("appregistry: app %s capability 名非法 %q", a.AppID, c.Name)
		}
		if seenCap[c.Name] {
			return fmt.Errorf("appregistry: app %s capability 重复 %q", a.AppID, c.Name)
		}
		seenCap[c.Name] = true
	}
	if len(a.LaunchTargets) < 1 {
		return fmt.Errorf("appregistry: app %s launch_targets 至少 1 项", a.AppID)
	}
	for _, ts := range [][]Target{a.LaunchTargets, a.ReceiptTargets} {
		for _, tg := range ts {
			if tg.TargetID == "" || !slugRe.MatchString(tg.TargetID) {
				return fmt.Errorf("appregistry: app %s target_id 非法 %q", a.AppID, tg.TargetID)
			}
			if seenTarget[tg.TargetID] {
				return fmt.Errorf("appregistry: target_id 全局重复 %q", tg.TargetID)
			}
			seenTarget[tg.TargetID] = true
			if !targetKindValid[tg.Kind] {
				return fmt.Errorf("appregistry: target %s kind 非法 %q", tg.TargetID, tg.Kind)
			}
			if err := validateTargetURL(tg.URL); err != nil {
				return fmt.Errorf("appregistry: target %s: %w", tg.TargetID, err)
			}
		}
	}
	return nil
}

// validateTargetURL 精确 https;仅 loopback(127.0.0.1/::1/localhost)允许 http;
// 禁止 query/fragment/userinfo;path 必填。
func validateTargetURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url 解析失败: %w", err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := strings.Trim(u.Hostname(), "[]")
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			return errors.New("http 仅允许 loopback")
		}
	default:
		return fmt.Errorf("scheme 非法 %q", u.Scheme)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("禁止 query/fragment/userinfo")
	}
	if u.Path == "" {
		return errors.New("path 必填")
	}
	return nil
}

// LookupApp 按 app_id 查应用。
func (m *Manifest) LookupApp(appID string) (App, bool) {
	for _, a := range m.Apps {
		if a.AppID == appID {
			return a, true
		}
	}
	return App{}, false
}

// ResolveTarget 按 (app, target_id, kind) 唯一解析登记 URL。
// 自由 URL、伪造 target、跨应用 target、kind 不匹配一律不命中。
func (m *Manifest) ResolveTarget(appID, targetID, kind string) (string, error) {
	a, ok := m.LookupApp(appID)
	if !ok {
		return "", fmt.Errorf("appregistry: app 未登记 %q", appID)
	}
	for _, t := range a.LaunchTargets {
		if t.TargetID == targetID {
			if t.Kind != kind {
				return "", fmt.Errorf("appregistry: target %s kind 不匹配", targetID)
			}
			return t.URL, nil
		}
	}
	for _, t := range a.ReceiptTargets {
		if t.TargetID == targetID {
			if t.Kind != kind {
				return "", fmt.Errorf("appregistry: target %s kind 不匹配", targetID)
			}
			return t.URL, nil
		}
	}
	return "", fmt.Errorf("appregistry: target 未登记 %q", targetID)
}

// FirstReceiptTarget 返回应用的第一个 receipt target(fail-closed:没有则错误)。
func (m *Manifest) FirstReceiptTarget(appID string) (Target, error) {
	a, ok := m.LookupApp(appID)
	if !ok {
		return Target{}, fmt.Errorf("appregistry: app 未登记 %q", appID)
	}
	if len(a.ReceiptTargets) == 0 {
		return Target{}, fmt.Errorf("appregistry: app %s 未登记 receipt target(无法回执,fail-closed)", appID)
	}
	return a.ReceiptTargets[0], nil
}
