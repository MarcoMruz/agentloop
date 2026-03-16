package memory

import (
	"os"
	"path/filepath"
	"testing"
)

// --- ExtractKeywords ---

func TestExtractKeywordsBasic(t *testing.T) {
	kw := ExtractKeywords("schedule a standup meeting with the team for tomorrow")
	found := map[string]bool{}
	for _, k := range kw {
		found[k] = true
	}
	if !found["standup"] {
		t.Error("expected 'standup' in keywords")
	}
	if !found["meeting"] {
		t.Error("expected 'meeting' in keywords")
	}
	if !found["tomorrow"] {
		t.Error("expected 'tomorrow' in keywords")
	}
	// stopwords must be filtered
	for _, stop := range []string{"the", "with", "for", "and"} {
		if found[stop] {
			t.Errorf("stopword %q should be filtered", stop)
		}
	}
}

func TestExtractKeywordsShortWordsFiltered(t *testing.T) {
	kw := ExtractKeywords("do it in an api call")
	found := map[string]bool{}
	for _, k := range kw {
		found[k] = true
	}
	for _, short := range []string{"do", "it", "in", "an"} {
		if found[short] {
			t.Errorf("short word %q should be filtered", short)
		}
	}
	if !found["call"] {
		t.Error("expected 'call' in keywords")
	}
}

func TestExtractKeywordsDedup(t *testing.T) {
	kw := ExtractKeywords("deploy deploy deploy the service")
	count := 0
	for _, k := range kw {
		if k == "deploy" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 'deploy' exactly once, got %d", count)
	}
}

func TestExtractKeywordsMaxCap(t *testing.T) {
	// Generate a long sentence with 20+ unique non-stopword words
	text := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda sigma omega upsilon phi chi psi rho tau omicron"
	kw := ExtractKeywords(text)
	if len(kw) > 15 {
		t.Errorf("expected at most 15 keywords, got %d", len(kw))
	}
}

// --- ExtractTopics ---

func TestExtractTopicsCode(t *testing.T) {
	topics := ExtractTopics("deploy the docker container to kubernetes cluster")
	found := map[string]bool{}
	for _, t := range topics {
		found[t] = true
	}
	if !found["deploy"] {
		t.Error("expected topic 'deploy'")
	}
	if !found["docker"] {
		t.Error("expected topic 'docker'")
	}
	if !found["kubernetes"] {
		t.Error("expected topic 'kubernetes'")
	}
}

func TestExtractTopicsPersonal(t *testing.T) {
	topics := ExtractTopics("schedule a meeting and send an email about the budget review")
	found := map[string]bool{}
	for _, t := range topics {
		found[t] = true
	}
	if !found["schedule"] {
		t.Error("expected topic 'schedule'")
	}
	if !found["meeting"] {
		t.Error("expected topic 'meeting'")
	}
	if !found["email"] {
		t.Error("expected topic 'email'")
	}
	if !found["budget"] {
		t.Error("expected topic 'budget'")
	}
	if !found["review"] {
		t.Error("expected topic 'review'")
	}
}

func TestExtractTopicsSubstringMatch(t *testing.T) {
	// "scheduling" should match "schedule" via substring
	topics := ExtractTopics("scheduling the team standup")
	found := map[string]bool{}
	for _, t := range topics {
		found[t] = true
	}
	if !found["schedule"] {
		t.Error("expected topic 'schedule' matched via substring in 'scheduling'")
	}
}

// --- summarizeEntry ---

func TestSummarizeEntryShort(t *testing.T) {
	s := summarizeEntry("fix the login bug")
	if s == "" {
		t.Error("expected non-empty summary")
	}
	if s != "fix the login bug" {
		t.Errorf("unexpected summary: %q", s)
	}
}

func TestSummarizeEntryTruncates(t *testing.T) {
	long := "this is a very long sentence that should definitely be truncated because it exceeds one hundred and twenty characters in total length right here yes it does"
	s := summarizeEntry(long)
	if len(s) > 120 {
		t.Errorf("summary exceeds 120 chars: len=%d", len(s))
	}
	if s[len(s)-3:] != "..." {
		t.Error("truncated summary should end with '...'")
	}
}

func TestSummarizeEntrySkipsHeaders(t *testing.T) {
	content := "# Header\n\nactual content here"
	s := summarizeEntry(content)
	if s == "# Header" || s == "Header" {
		t.Error("summary should skip markdown headers")
	}
	if s != "actual content here" {
		t.Errorf("unexpected summary: %q", s)
	}
}

func TestSummarizeEntryStripsMarkdown(t *testing.T) {
	content := "**created** the `auth` middleware"
	s := summarizeEntry(content)
	if s == "" {
		t.Error("expected non-empty summary")
	}
	// Bold and backtick markers should be removed
	for _, marker := range []string{"**", "`"} {
		for i := 0; i <= len(s)-len(marker); i++ {
			if s[i:i+len(marker)] == marker {
				t.Errorf("markdown marker %q not stripped from summary", marker)
			}
		}
	}
}

// --- scoreEntry ---

func TestScoreEntryFullMatch(t *testing.T) {
	entry := IndexEntry{
		Keywords: []string{"standup", "meeting", "team"},
		Topics:   []string{"meeting"},
	}
	score := scoreEntry([]string{"standup", "meeting", "team"}, []string{"meeting"}, entry)
	// 3/3 keyword match + topic bonus = 1.2
	if score < 1.0 {
		t.Errorf("expected high score for full match, got %.2f", score)
	}
}

func TestScoreEntryPartialMatch(t *testing.T) {
	entry := IndexEntry{
		Keywords: []string{"standup", "meeting"},
		Topics:   []string{},
	}
	score := scoreEntry([]string{"standup", "meeting", "budget"}, []string{}, entry)
	// 2/3 overlap = 0.667
	if score < 0.5 || score > 0.8 {
		t.Errorf("expected partial match score ~0.67, got %.2f", score)
	}
}

func TestScoreEntryNoMatch(t *testing.T) {
	entry := IndexEntry{
		Keywords: []string{"invoice", "vendor", "payment"},
		Topics:   []string{"finance"},
	}
	score := scoreEntry([]string{"standup", "meeting", "team"}, []string{"meeting"}, entry)
	if score != 0 {
		t.Errorf("expected 0 score for no keyword overlap, got %.2f", score)
	}
}

func TestScoreEntryTopicBoostOnly(t *testing.T) {
	// No keyword overlap but matching topic → small boost only, not > 0.2
	entry := IndexEntry{
		Keywords: []string{"invoice", "vendor"},
		Topics:   []string{"finance"},
	}
	score := scoreEntry([]string{"budget", "quarterly"}, []string{"finance"}, entry)
	// 0 keyword overlap, topic match gives 0.2 only IF keyword overlap > 0.
	// Our implementation: score = overlap/total + topic_bonus
	// overlap=0, so base=0, topic bonus still applies → 0.2
	// This is intentional: same domain context is marginally useful
	if score < 0 || score > 0.3 {
		t.Errorf("expected topic-only boost ~0.2, got %.2f", score)
	}
}

func TestScoreEntryEmptyTask(t *testing.T) {
	entry := IndexEntry{Keywords: []string{"auth", "token"}, Topics: []string{"auth"}}
	score := scoreEntry([]string{}, []string{}, entry)
	if score != 0 {
		t.Errorf("expected 0 score for empty task keywords, got %.2f", score)
	}
}

// --- appendIndexEntry / loadIndex round-trip ---

func TestIndexRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.idx.json")

	e1 := IndexEntry{Timestamp: "10:00:00", Role: "user", Keywords: []string{"auth"}, Topics: []string{"auth"}, Summary: "setup auth"}
	e2 := IndexEntry{Timestamp: "10:01:00", Role: "assistant", Keywords: []string{"auth", "token"}, Topics: []string{"auth"}, Summary: "created token handler"}

	if err := appendIndexEntry(path, e1); err != nil {
		t.Fatalf("appendIndexEntry failed: %v", err)
	}
	if err := appendIndexEntry(path, e2); err != nil {
		t.Fatalf("appendIndexEntry failed: %v", err)
	}

	idx := loadIndex(path)
	if len(idx.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(idx.Entries))
	}
	if idx.Entries[0].Summary != "setup auth" {
		t.Errorf("unexpected summary: %q", idx.Entries[0].Summary)
	}
	if idx.Entries[1].Role != "assistant" {
		t.Errorf("unexpected role: %q", idx.Entries[1].Role)
	}
}

func TestLoadIndexMissingFile(t *testing.T) {
	idx := loadIndex("/nonexistent/path/file.idx.json")
	if len(idx.Entries) != 0 {
		t.Error("expected empty index for missing file")
	}
}

// --- GetContextForUserWithTask integration ---

func TestGetContextForUserWithTaskRelevance(t *testing.T) {
	vaultPath := t.TempDir()
	engine := NewEngine(vaultPath, 2000, "rolling", 7)

	userId := "testuser"

	// Seed: coding turn
	if err := engine.conversations.Append(userId, "user", "fix the authentication middleware bug in the API", ""); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := engine.conversations.Append(userId, "assistant", "fixed null check in auth middleware, deployed to staging", ""); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Seed: personal assistant turn
	if err := engine.conversations.Append(userId, "user", "check my calendar for meetings scheduled next week", ""); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := engine.conversations.Append(userId, "assistant", "found 3 meetings next week: standup Monday, review Wednesday, interview Friday", ""); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Task about auth → should surface auth entries, not calendar
	ctx, err := engine.GetContextForUserWithTask(userId, "there is a bug in the auth token refresh")
	if err != nil {
		t.Fatalf("GetContextForUserWithTask failed: %v", err)
	}
	if ctx == "" {
		t.Fatal("expected non-empty context")
	}

	// Auth-related summaries should appear
	if !containsAny(ctx, "auth", "middleware", "authentication") {
		t.Errorf("expected auth-related content in context, got:\n%s", ctx)
	}

	// Calendar task → should surface calendar entries
	engine.cache.Delete("ctx:" + userId + ":check_my_calendar_for_meetings_sche")
	ctx2, err := engine.GetContextForUserWithTask(userId, "what meetings do I have scheduled for next week")
	if err != nil {
		t.Fatalf("GetContextForUserWithTask failed: %v", err)
	}
	if !containsAny(ctx2, "meeting", "calendar", "standup", "schedule") {
		t.Errorf("expected calendar-related content in context, got:\n%s", ctx2)
	}
}

func TestGetContextForUserWithTaskFallback(t *testing.T) {
	vaultPath := t.TempDir()
	// Remove index directory to simulate no index files
	engine := NewEngine(vaultPath, 2000, "rolling", 7)
	userId := "newuser"

	// No index seeded — should fall back gracefully (empty or profile-only context)
	ctx, err := engine.GetContextForUserWithTask(userId, "help me with something")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = ctx // empty context is acceptable for a new user
}

func TestGetContextForUserWithTaskCacheHit(t *testing.T) {
	vaultPath := t.TempDir()
	engine := NewEngine(vaultPath, 2000, "rolling", 7)
	userId := "cacheuser"

	if err := engine.conversations.Append(userId, "user", "check the deployment status on kubernetes", ""); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	task := "what is the deployment status"
	ctx1, _ := engine.GetContextForUserWithTask(userId, task)
	ctx2, _ := engine.GetContextForUserWithTask(userId, task)

	if ctx1 != ctx2 {
		t.Error("expected identical context on cache hit")
	}
}

// --- helpers ---

func containsAny(s string, substrs ...string) bool {
	lower := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		lower[i] = c
	}
	ls := string(lower)
	for _, sub := range substrs {
		if len(sub) == 0 {
			continue
		}
		for i := 0; i <= len(ls)-len(sub); i++ {
			if ls[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

// Ensure index sidecar files are created alongside markdown files
func TestConversationIndexSidecarCreated(t *testing.T) {
	vaultPath := t.TempDir()
	cs := NewConversationStore(vaultPath, 7)

	if err := cs.Append("marco", "user", "send an email to the team about the sprint review", ""); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Check that at least one .idx.json file was created
	dir := filepath.Join(vaultPath, "memory", "contexts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	found := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected .idx.json sidecar file to be created after Append()")
	}
}

func TestGetRecentIndexedReturnsSummaries(t *testing.T) {
	vaultPath := t.TempDir()
	cs := NewConversationStore(vaultPath, 7)

	if err := cs.Append("marco", "user", "schedule a standup meeting", ""); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := cs.Append("marco", "assistant", "created the calendar invite", ""); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	indexed, err := cs.GetRecentIndexed("marco", 10)
	if err != nil {
		t.Fatalf("GetRecentIndexed failed: %v", err)
	}
	if len(indexed) < 2 {
		t.Fatalf("expected at least 2 indexed entries, got %d", len(indexed))
	}
	for _, e := range indexed {
		if e.Summary == "" {
			t.Error("expected non-empty summary for indexed entry")
		}
		if e.Role == "" {
			t.Error("expected non-empty role")
		}
	}
}
