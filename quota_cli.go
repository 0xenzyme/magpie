package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

const quotaUsage = `usage: magpie quota [<provider>…] [--json]
  what is left of every subscription, plan and key magpie has: each window's use and
  when it starts again, and each key's balance, asked of the vendors now (or less than
  a minute ago). --json is for scripts and agents; the gateway answers the same at
  GET http://127.0.0.1:3425/v1/magpie/quotas`

// quotaCmd: magpie quota [<provider>…] [--json]
func quotaCmd(args []string) error {
	asJSON := false
	var only []string
	for _, a := range args[1:] {
		switch a {
		case "--json", "-j":
			asJSON = true
		case "help", "-h", "--help":
			fmt.Println(quotaUsage)
			return nil
		default:
			only = append(only, strings.ToLower(a))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	qs := []provider.Quota{}
	for _, q := range provider.QuotaReport(ctx, time.Now()) {
		if len(only) == 0 || quotaMatches(q, only) {
			qs = append(qs, q)
		}
	}
	qs, err := withUntold(qs, only)
	if err != nil {
		return err
	}
	if asJSON {
		b, _ := json.MarshalIndent(qs, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if len(qs) == 0 {
		if len(only) > 0 {
			return fmt.Errorf("no subscription, plan or key balance of %s", strings.Join(only, ", "))
		}
		fmt.Println(muted.Render("nothing to tell ·"), "a signed-in subscription, a coding plan or a key whose vendor tells its balance shows up here")
		return nil
	}
	width := 0
	for _, q := range qs {
		width = max(width, len([]rune(quotaTitle(q))))
	}
	for _, q := range qs {
		line := fmt.Sprintf("%-*s  %s", width, quotaTitle(q), muted.Render(fmt.Sprintf("%-12s", q.Kind)))
		for _, w := range q.Windows {
			line += "  " + quotaCell(w)
		}
		if q.Balance != "" {
			line += "  " + bold.Render(q.Balance) + muted.Render(" left")
		}
		if q.Error != "" {
			line += "  " + muted.Render(q.Error)
		}
		fmt.Println(line)
	}
	fmt.Println(faint.Render("  % is how much of a window is used · ↻ when it starts again · --json for scripts, or GET /v1/magpie/quotas on the gateway"))
	return nil
}

// quotaTitle is a quota's line head: its provider, plan and account.
func quotaTitle(q provider.Quota) string {
	t := q.Provider
	if q.Plan != "" {
		t += " · " + q.Plan
	}
	if s := provider.PlanTerm(q.Until, q.Renew); s != "" {
		t += " · " + s
	}
	if q.User != "" {
		t += " · " + q.User
	}
	return t
}

func quotaMatches(q provider.Quota, only []string) bool {
	for _, o := range only {
		if strings.EqualFold(q.Provider, o) || strings.EqualFold(q.Name, o) || strings.EqualFold(q.Kind, o) {
			return true
		}
	}
	return false
}

// withUntold adds a line for each provider named that told nothing, saying
// why, rather than leaving it out as if it weren't there; a name that is no
// provider at all is an error.
func withUntold(qs []provider.Quota, only []string) ([]provider.Quota, error) {
	for _, o := range only {
		if o == "subscription" || o == "plan" || o == "balance" || slices.ContainsFunc(qs, func(q provider.Quota) bool { return quotaMatches(q, []string{o}) }) {
			continue
		}
		p, err := provider.Find(o)
		if err != nil {
			return nil, err
		}
		qs = append(qs, provider.Quota{Provider: p.ID, Name: p.Name, Kind: "balance", Windows: []provider.QuotaSpan{},
			Error: "not configured · no balance endpoint known for it; set Balance URL and Balance field in its editor"})
	}
	return qs, nil
}
