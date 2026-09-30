package provider

// A plugin's accounts show their allowance as the built-ins' do: on the
// usage page, in the menu bar, beside each account, and to the gateway,
// which passes over one used up. The plugin tells it (auth.usage, see
// internal/plugin/host.js); a built-in moved onto its plugin keeps its
// card's name and icon.

import (
	"context"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/plugin"
)

// movedCards are the names and icons the built-ins' usage cards have.
var movedCards = map[string][2]string{
	"zed":             {"Zed", "zed"},
	"qoder":           {"Qoder", "qoder"},
	"factory":         {"Factory", "factory"},
	MiMoID:            {"Xiaomi MiMo", "mimocode"},
	CommandCodePlanID: {"Command Code", "commandcode"},
	"kiro":            {"Kiro", "kiro-color"},
	"zcode":           {"ZCode", "zcode"},
	"workbuddy":       {"WorkBuddy", "workbuddy-color"},
	"cursor":          {"Cursor", "cursor"},
	"grok":            {"Grok (SuperGrok)", "xai"},
	"devin":           {"Devin", "devin"},
}

// pluginCard is the name and icon pp's usage cards show.
func pluginCard(pp plugin.Provider) (string, string) {
	if c, ok := movedCards[pp.ID]; ok && Moved(pp.ID) {
		return c[0], c[1]
	}
	name := pp.Name
	if name == "" {
		name = pp.ID
	}
	return name, plugin.Icon(pp.Spec, pp.ID)
}

// pluginUsageLogins are the accounts of pp whose allowance can be asked.
func pluginUsageLogins(pp plugin.Provider) []Login {
	if !pp.Usage || !pp.SignedIn || movingNow(pp.ID) {
		return nil
	}
	return pluginLoginList(pp)
}

// pluginLoginQuota is the allowance of one of a plugin provider's
// accounts, l.Agent being "plugin:" and its id.
func pluginLoginQuota(ctx context.Context, l Login) SubscriptionQuota {
	pp, ok := pluginOfAgent(l.Agent)
	name, icon := pluginCard(pp)
	q := SubscriptionQuota{Provider: PluginID(pp.ID), Name: name, Icon: icon, Plan: l.Plan, User: l.User, Windows: []QuotaWindow{}}
	if !ok {
		q.Error = "no such plugin provider"
		return q
	}
	key := ""
	for _, x := range pluginLogins(pp) {
		if strings.EqualFold(x.User, l.User) {
			key = x.acct.Key
		}
	}
	if key == "" {
		q.Error = "not signed in"
		return q
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	u, err := plugin.AccountUsage(ctx, pp.ID, key)
	if err != nil {
		q.Error = err.Error()
		return q
	}
	return quotaOfPlugin(q, u)
}

// quotaOfPlugin fills q with what the plugin told.
func quotaOfPlugin(q SubscriptionQuota, u plugin.Usage) SubscriptionQuota {
	if u.Plan != "" {
		q.Plan = u.Plan
	}
	if u.User != "" {
		q.User = u.User
	}
	q.Balance, q.Renew, q.Error = u.Balance, u.Renew, u.Error
	if t, err := time.Parse(time.RFC3339, u.Until); err == nil {
		q.Until = &t
	}
	if r := u.Resets; r != nil {
		q.Resets = &ResetCredits{Count: r.Count, ByWindow: r.ByWindow, FiveHour: r.FiveHour, Weekly: r.Weekly}
		if t, err := time.Parse(time.RFC3339, r.Until); err == nil {
			q.Resets.Until = &t
		}
	}
	for _, x := range u.Windows {
		w := QuotaWindow{Name: x.Name, Used: x.Used, ResetSecs: x.ResetSecs, Display: x.Display,
			Span: time.Duration(x.Span * float64(time.Second)), Model: strings.ToLower(x.Model), Aside: x.Aside}
		if t, err := time.Parse(time.RFC3339, x.ResetsAt); err == nil {
			w.ResetsAt = &t
		}
		if set := lowerSet(x.Models); set != nil {
			w.matches = func(model string) bool { return set[strings.ToLower(model)] }
		} else if set := lowerSet(x.NotModels); set != nil {
			w.matches = func(model string) bool { return !set[strings.ToLower(model)] }
		}
		q.Windows = append(q.Windows, w)
	}
	return q
}

func lowerSet(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	m := map[string]bool{}
	for _, id := range ids {
		m[strings.ToLower(id)] = true
	}
	return m
}

// pluginUsageFetches are a card's fetch for each plugin account that
// tells its allowance.
func pluginUsageFetches(via func(string) context.Context, hidden map[string]bool) []func() SubscriptionQuota {
	var out []func() SubscriptionQuota
	for _, pp := range plugin.Cached() {
		id := PluginID(pp.ID)
		if hidden[id] {
			continue
		}
		ls := pluginUsageLogins(pp)
		if len(ls) == 0 {
			continue
		}
		name, icon := pluginCard(pp)
		out = append(out, perLogin(via(id), ls, name, icon)...)
	}
	return out
}

// UsageAgent is the agent an account's allowance is asked for by
// (LoginUsage, Allowances): a plugin's accounts by their provider's.
func (a *Account) UsageAgent() string {
	if a.plugin != nil {
		return "plugin:" + a.plugin.ID
	}
	return a.Agent
}
