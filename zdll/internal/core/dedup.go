package core

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// BloomDeduplicator is an O(1) probabilistic pre-filter.
type BloomDeduplicator struct {
	size      int
	numHashes int
	bits      []byte
	count     int
}

func NewBloomDeduplicator(size int, numHashes int) *BloomDeduplicator {
	if size <= 0 {
		size = 10000
	}
	if numHashes <= 0 {
		numHashes = 7
	}
	return &BloomDeduplicator{
		size:      size,
		numHashes: numHashes,
		bits:      make([]byte, size/8+1),
	}
}

func (b *BloomDeduplicator) hashPositions(text string) []int {
	h := fnv.New64a()
	h.Write([]byte(text))
	h1 := h.Sum64()
	h.Reset()
	h.Write([]byte("salt" + text))
	h2 := h.Sum64()

	positions := make([]int, b.numHashes)
	for i := 0; i < b.numHashes; i++ {
		positions[i] = int((h1 + uint64(i)*h2) % uint64(b.size))
	}
	return positions
}

func (b *BloomDeduplicator) Add(text string) {
	for _, p := range b.hashPositions(text) {
		b.bits[p/8] |= 1 << (p % 8)
	}
	b.count++
}

func (b *BloomDeduplicator) MightContain(text string) bool {
	for _, p := range b.hashPositions(text) {
		if b.bits[p/8]&(1<<(p%8)) == 0 {
			return false
		}
	}
	return true
}

func (b *BloomDeduplicator) Stats() map[string]int {
	bitsUsed := 0
	for _, by := range b.bits {
		for i := 0; i < 8; i++ {
			if by&(1<<i) != 0 {
				bitsUsed++
			}
		}
	}
	return map[string]int{
		"inserted":  b.count,
		"bits_used": bitsUsed,
	}
}

// securitySynonyms normalizes common security terms for deduplication.
var securitySynonyms = map[string]string{
	"xss":             "cross_site_scripting",
	"cross site":      "cross_site_scripting",
	"cross-site":      "cross_site_scripting",
	"sqli":            "sql_injection",
	"sql injection":   "sql_injection",
	"sql-injection":   "sql_injection",
	"rce":             "remote_code_execution",
	"lfi":             "local_file_include",
	"rfi":             "remote_file_include",
	"ssrf":            "server_side_request_forgery",
	"csrf":            "cross_site_request_forgery",
	"idor":            "insecure_direct_object_reference",
	"deserialization": "deserialize",
	"deserialize":     "deserialize",
	"unserialize":     "deserialize",
	"buffer overflow": "buffer_overflow",
	"oob":             "out_of_bounds",
	"out-of-bounds":   "out_of_bounds",
}

var (
	dedupNormalizer = regexp.MustCompile(`[^a-z0-9\x{4e00}-\x{9fff}\s/._-]`)
	stopWords       = map[string]struct{}{
		"the": {}, "a": {}, "an": {}, "is": {}, "are": {}, "was": {}, "were": {},
		"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {}, "do": {},
		"does": {}, "did": {}, "will": {}, "would": {}, "could": {}, "should": {},
		"may": {}, "might": {}, "must": {}, "shall": {}, "can": {}, "need": {},
		"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {}, "with": {},
		"by": {}, "from": {}, "as": {}, "into": {}, "through": {}, "during": {},
		"before": {}, "after": {}, "above": {}, "below": {}, "between": {}, "among": {},
		"and": {}, "or": {}, "but": {}, "so": {}, "yet": {}, "because": {}, "although": {},
		"this": {}, "that": {}, "these": {}, "those": {}, "it": {}, "its": {}, "they": {},
		"them": {}, "their": {}, "we": {}, "our": {}, "us": {}, "i": {}, "me": {}, "my": {},
		"you": {}, "your": {}, "he": {}, "she": {}, "his": {}, "her": {}, "him": {},
	}
)

func normalizeText(text string) []string {
	lower := strings.ToLower(text)
	lower = dedupNormalizer.ReplaceAllString(lower, " ")
	// Apply security term synonyms as whole-word replacements only, so that
	// short forms like "rfi" do not corrupt unrelated words (e.g. "dockerfile").
	for from, to := range securitySynonyms {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(from) + `\b`)
		lower = re.ReplaceAllString(lower, to)
	}
	fields := strings.Fields(lower)
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{})
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := stopWords[f]; ok {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func bigrams(tokens []string) map[string]struct{} {
	bi := make(map[string]struct{})
	for i := 0; i < len(tokens)-1; i++ {
		bi[tokens[i]+" "+tokens[i+1]] = struct{}{}
	}
	return bi
}

func trigrams(tokens []string) map[string]struct{} {
	tri := make(map[string]struct{})
	for i := 0; i < len(tokens)-2; i++ {
		tri[tokens[i]+" "+tokens[i+1]+" "+tokens[i+2]] = struct{}{}
	}
	return tri
}

func charTrigrams(text string) map[string]struct{} {
	s := strings.ToLower(text)
	tri := make(map[string]struct{})
	for i := 0; i < len(s)-2; i++ {
		tri[s[i:i+3]] = struct{}{}
	}
	return tri
}

func jaccard(a, b map[string]struct{}) float64 {
	// Empty feature sets provide no evidence of similarity; treat as 0 instead
	// of 1 to avoid merging unrelated short strings.
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

func structuralFingerprint(tokens []string) string {
	return strings.Join(tokens, "|")
}

func extractAnchors(text string) (functions, files []string) {
	// function names snake_case / camelCase
	reFn := regexp.MustCompile(`\b[a-zA-Z_][a-zA-Z0-9_]*\s*\(`)
	for _, m := range reFn.FindAllString(text, -1) {
		fn := strings.TrimSpace(strings.TrimSuffix(m, "("))
		functions = append(functions, strings.ToLower(fn))
	}
	// file names like foo.py, bar.go
	reFile := regexp.MustCompile(`\b[a-zA-Z0-9_./-]+\.[a-zA-Z0-9]+\b`)
	for _, m := range reFile.FindAllString(text, -1) {
		files = append(files, strings.ToLower(m))
	}
	return
}

// findSimilarHypothesis implements the five-layer semantic dedup pipeline.
// Returns the most similar existing hypothesis ID and the similarity score.
func findSimilarHypothesis(bb *Blackboard, description string, threshold float64) (string, float64) {
	if threshold <= 0 {
		threshold = 0.55
	}
	tokens := normalizeText(description)
	newBigrams := bigrams(tokens)
	newUnigrams := make(map[string]struct{})
	for _, t := range tokens {
		newUnigrams[t] = struct{}{}
	}
	newStruct := structuralFingerprint(tokens)
	newCharTri := charTrigrams(description)
	newFuncs, newFiles := extractAnchors(description)

	bestID := ""
	bestScore := 0.0

	for _, h := range bb.Hypotheses {
		hTokens := normalizeText(h.Description)
		hBigrams := bigrams(hTokens)
		hUnigrams := make(map[string]struct{})
		for _, t := range hTokens {
			hUnigrams[t] = struct{}{}
		}

		// Layer 0: anchor match
		hFuncs, hFiles := extractAnchors(h.Description)
		funcOverlap := overlapCount(newFuncs, hFuncs)
		fileOverlap := overlapCount(newFiles, hFiles)
		uniOverlap := 0
		for u := range newUnigrams {
			if _, ok := hUnigrams[u]; ok {
				uniOverlap++
			}
		}
		minLen := minInt(len(newUnigrams), len(hUnigrams))
		anchorScore := 0.0
		if minLen > 0 {
			uniRatio := float64(uniOverlap) / float64(minLen)
			// Lower the unigram bar when there is a file/function anchor or a
			// recognized vulnerability-class keyword. This catches paraphrased
			// duplicates that describe the same bug class in the same area.
			if funcOverlap > 0 || fileOverlap > 0 {
				if uniRatio >= 0.20 {
					anchorScore = 1.0
				}
			} else if containsVulnKeyword(description) && uniRatio >= 0.25 {
				anchorScore = 1.0
			}
		}
		if anchorScore >= threshold {
			bestID = h.ID
			bestScore = anchorScore
			continue
		}

		// Layer 1: structural fingerprint
		if structuralFingerprint(hTokens) == newStruct {
			if anchorScore >= threshold || bestScore < 1.0 {
				bestID = h.ID
				bestScore = 1.0
			}
			continue
		}

		// Layer 2: bigram Jaccard
		bigramScore := jaccard(newBigrams, hBigrams)
		if bigramScore >= threshold {
			if bigramScore > bestScore {
				bestID = h.ID
				bestScore = bigramScore
			}
			continue
		}

		// Layer 3: unigram Jaccard
		unigramScore := jaccard(newUnigrams, hUnigrams)
		if unigramScore >= threshold+0.10 {
			if unigramScore > bestScore {
				bestID = h.ID
				bestScore = unigramScore
			}
			continue
		}

		// Layer 4: char trigram fallback
		hCharTri := charTrigrams(h.Description)
		charScore := jaccard(newCharTri, hCharTri)
		if charScore >= 0.70 {
			if charScore > bestScore {
				bestID = h.ID
				bestScore = charScore
			}
		}
	}

	return bestID, bestScore
}

func overlapCount(a, b []string) int {
	m := make(map[string]struct{})
	for _, x := range a {
		m[x] = struct{}{}
	}
	c := 0
	for _, x := range b {
		if _, ok := m[x]; ok {
			c++
		}
	}
	return c
}

func containsVulnKeyword(text string) bool {
	keywords := []string{
		"vulnerability", "vuln", "exploit", "overflow", "injection", "sqli", "xss",
		"rce", "csrf", "ssrf", "lfi", "rfi", "idor", "deserialize", "unserialize",
		"authentication", "authorization", "path traversal", "command injection",
		"buffer", "out-of-bounds", "oob", "race condition", "dos", "denial",
	}
	lower := strings.ToLower(text)
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// classifyHypothesisPolarity rejects negative claims like "there is no vulnerability".
func classifyHypothesisPolarity(description string) string {
	lower := strings.ToLower(description)
	negativePatterns := []string{
		"there is no ", "no vulnerability", "not vulnerable", "does not have",
		"doesn't have", "is not ", "isn't ", "cannot be ", "can't be ",
		"unlikely to be ", "no evidence of", "no sign of", "no indication of",
	}
	for _, p := range negativePatterns {
		if strings.Contains(lower, p) {
			return "negative"
		}
	}
	return "positive"
}

// isTaskDuplicate checks whether newDescription is a duplicate of an existing
// task within the same hypothesis and drone role. The caller (AddTask) already
// ensures hypothesis ID and role match, so this function only compares the
// normalized descriptions using exact match and near-substring containment,
// matching the Python engine behaviour.
func isTaskDuplicate(existing *DroneTask, newDescription string) bool {
	a := normalizeForTask(existing.Description)
	b := normalizeForTask(newDescription)
	if a == b {
		return true
	}
	// Near-substring containment: one description is contained in the other
	// and their lengths differ by at most 10 runes.
	if strings.Contains(a, b) || strings.Contains(b, a) {
		if absInt(len([]rune(a))-len([]rune(b))) <= 10 {
			return true
		}
	}
	return false
}

func normalizeForTask(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Han, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// isFindingDuplicate checks duplicate by hypothesis_id or title token Jaccard.
func isFindingDuplicate(a, b *Finding) bool {
	if a.HypothesisID != "" && a.HypothesisID == b.HypothesisID {
		return true
	}
	ta := normalizeText(a.Title)
	tb := normalizeText(b.Title)
	ua := make(map[string]struct{})
	ub := make(map[string]struct{})
	for _, t := range ta {
		ua[t] = struct{}{}
	}
	for _, t := range tb {
		ub[t] = struct{}{}
	}
	return jaccard(ua, ub) >= 0.6
}

// mergeSeverity picks the more severe of two severity strings.
func mergeSeverity(a, b string) string {
	if SeverityRank(a) >= SeverityRank(b) {
		return a
	}
	return b
}

// mergeEvidence concatenates evidence with a separator.
func mergeEvidence(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return fmt.Sprintf("%s\n\n---\n\n%s", a, b)
}

// DeduplicateFindings merges findings whose titles and descriptions are highly
// similar. It is intended as a second-pass filter for report rendering, where
// title-only deduplication is not enough.
func DeduplicateFindings(findings []*Finding) []*Finding {
	if len(findings) <= 1 {
		return findings
	}
	var out []*Finding
	for _, f := range findings {
		merged := false
		for _, existing := range out {
			if findingsSimilar(existing, f) {
				if SeverityRank(f.Severity) > SeverityRank(existing.Severity) {
					existing.Severity = f.Severity
				}
				existing.Evidence = mergeEvidence(existing.Evidence, f.Evidence)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, f)
		}
	}
	return out
}

func findingsSimilar(a, b *Finding) bool {
	if a.HypothesisID != "" && a.HypothesisID == b.HypothesisID {
		return true
	}
	ta := make(map[string]struct{})
	for _, t := range normalizeText(a.Title + " " + a.Description) {
		ta[t] = struct{}{}
	}
	tb := make(map[string]struct{})
	for _, t := range normalizeText(b.Title + " " + b.Description) {
		tb[t] = struct{}{}
	}
	return jaccard(ta, tb) >= 0.55
}
