package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/yetone/magpie/internal/access"
	"github.com/yetone/magpie/internal/provider"
)

// A gateway key may be held to some models (#882, access.Key.Models): it
// is shown only those in the model lists, and a request of it for another
// is refused before any provider is asked. A model is held to by its
// provider's id and its own, "<provider>/<model>", whatever the agent
// called it; a routing group passes only when every model in it does.

// magpieChoseKey marks, in a request's context, a call magpie makes for a
// model the user picked in its settings rather than the caller (a web
// search's, an image description's, a Codex title's): the key's models
// don't hold it.
type magpieChoseKey struct{}

func magpieChose(ctx context.Context) context.Context {
	return context.WithValue(ctx, magpieChoseKey{}, true)
}

// keyHolds is the calling key's models, when they hold this request.
func keyHolds(r *http.Request) (access.Identity, bool) {
	who := access.Caller(r.Context())
	if !who.Restricted() {
		return who, false
	}
	if chose, _ := r.Context().Value(magpieChoseKey{}).(bool); chose {
		return who, false
	}
	return who, true
}

// modelAllowed says the key may use provider p's model.
func modelAllowed(who access.Identity, p provider.Provider, model string) bool {
	return who.Allows(p.ID + "/" + model)
}

// memberAllowed says the key may use a routing group's member.
func memberAllowed(who access.Identity, m provider.Member) bool {
	ids := []string{m.Provider.ID + "/" + m.Model}
	if n := len(m.Path); n > 0 {
		ids = append(ids, m.Path[n-1])
	}
	return who.Allows(ids...)
}

// membersAllowed says the key may use every member of a routing group:
// one it may use only some of is the key's no more than an empty one.
func membersAllowed(who access.Identity, ms []provider.Member) bool {
	if len(ms) == 0 {
		return !who.Restricted()
	}
	for _, m := range ms {
		if !memberAllowed(who, m) {
			return false
		}
	}
	return true
}

// entryAllowed says the key is shown a model of the catalog.
func entryAllowed(who access.Identity, e provider.Entry) bool {
	if !who.Restricted() {
		return true
	}
	if e.Group != "" {
		_, ms, ok := provider.FindGroup(e.ID)
		return ok && membersAllowed(who, ms)
	}
	return modelAllowed(who, e.Provider, e.Model)
}

// keyAllowed filters the catalog to what the calling key may use.
func keyAllowed(r *http.Request, es []provider.Entry) []provider.Entry {
	who, held := keyHolds(r)
	if !held {
		return es
	}
	out := make([]provider.Entry, 0, len(es))
	for _, e := range es {
		if entryAllowed(who, e) {
			out = append(out, e)
		}
	}
	return out
}

// allowedCandidates leaves out of a plan the providers' models the key
// may not use: a model's fallbacks are other models.
func allowedCandidates(who access.Identity, cs []candidate) []candidate {
	out := cs[:0:0]
	for _, c := range cs {
		if modelAllowed(who, c.p, c.model) {
			out = append(out, c)
		}
	}
	return out
}

// keyModelError is what a request for a model its key may not use is told.
func keyModelError(who access.Identity, model string) string {
	return fmt.Sprintf("The gateway key %q may not use %s; it may use %s. Change the key's models in magpie's Gateway page, or use a model it has.",
		who.KeyName, model, strings.Join(who.Models, ", "))
}
