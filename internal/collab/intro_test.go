package collab

import "testing"

func TestFallbackIntroContainsRoleName(t *testing.T) {
	for _, id := range MainChannelRoster {
	 intro := FallbackIntro(id)
	 if intro == "" {
	  t.Fatalf("empty fallback for %s", id)
	 }
	 name := DisplayName(id)
	 if name == "" {
	  t.Fatalf("empty display name for %s", id)
	 }
	}
}

func TestAssertionKeywordsNonEmpty(t *testing.T) {
	for _, id := range MainChannelRoster {
	 kw := AssertionKeywords(id)
	 if len(kw) == 0 {
	  t.Fatalf("no keywords for %s", id)
	 }
	}
}
