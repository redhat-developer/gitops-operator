package notificationsconfiguration

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
)

// DefaultNotificationsConfigurationName is the well-known name of the
// operator-managed NotificationsConfiguration CR.
const DefaultNotificationsConfigurationName = "default-notifications-configuration"

const (
	// contextConfigMapKey is the ConfigMap data key for the serialized context.
	contextConfigMapKey = "context"

	// subscriptionsConfigMapKey is the Subscriptions map key whose YAML-list
	// value receives special concatenation handling, and also the ConfigMap
	// data key where the merged list is stored.
	subscriptionsConfigMapKey = "subscriptions"
)

// MergeConflict records a single key-level precedence resolution where two
// NotificationsConfiguration CRs defined the same entry with different values.
type MergeConflict struct {
	// Key is the conflicting entry key. For flat fields this is the ConfigMap
	// data key. For context this is the individual context entry key.
	Key string

	// ExistingCR is the metadata.name of the CR whose value is preserved.
	ExistingCR string

	// ConflictingCR is the metadata.name of the CR whose value was ignored.
	ConflictingCR string
}

// MergeDuplicate records a same-value duplicate where two CRs define the
// same key with identical values. The value is kept; the duplicate is logged.
type MergeDuplicate struct {
	Key         string
	FirstCR     string
	DuplicateCR string
}

// SkippedCR records a CR that was excluded from the merge because it
// contains invalid configuration (reserved key, malformed YAML, etc.).
type SkippedCR struct {
	Name   string
	Reason string
}

// MergeResult holds the output of MergeNotificationsConfigurations.
type MergeResult struct {
	// Data is the desired ConfigMap.Data map. It is never nil when err is nil,
	// but may be empty when no active CRs contribute entries.
	Data map[string]string

	// Conflicts records key-level precedence resolutions where two CRs
	// defined the same entry with different values.
	Conflicts []MergeConflict

	// Duplicates records same-value duplicates where two CRs define the
	// same key with identical values.
	Duplicates []MergeDuplicate

	// SkippedCRs records CRs that were excluded from the merge because
	// they contain invalid configuration.
	SkippedCRs []SkippedCR
}

// entryMerger collects key/value entries from multiple CRs and records
// conflicts (same key, different values) and duplicates (same key, same value).
// Oldest CR wins because entries are added in precedence order.
type entryMerger struct {
	data       map[string]string
	owners     map[string]string
	conflicts  []MergeConflict
	duplicates []MergeDuplicate
}

func newEntryMerger() *entryMerger {
	return &entryMerger{
		data:   make(map[string]string),
		owners: make(map[string]string),
	}
}

func (m *entryMerger) add(key, value, crName string) {
	if currentOwner, exists := m.owners[key]; exists {
		if m.data[key] == value {
			m.duplicates = append(m.duplicates, MergeDuplicate{
				Key: key, FirstCR: currentOwner, DuplicateCR: crName,
			})
		} else {
			m.conflicts = append(m.conflicts, MergeConflict{
				Key: key, ExistingCR: currentOwner, ConflictingCR: crName,
			})
		}
		return
	}
	m.data[key] = value
	m.owners[key] = crName
}

// MergeNotificationsConfigurations merges all CRs in a namespace into one
// ConfigMap.Data map.
//
// Merge rules:
//   - Flat fields (Triggers, Templates, Services, non-reserved Subscriptions)
//     go directly into ConfigMap.Data.
//   - Context entries are serialized into a single "context" YAML key.
//   - Subscriptions["subscriptions"] lists are concatenated across CRs.
//   - CRs with a non-nil DeletionTimestamp are excluded.
//   - CRs with invalid configuration are skipped and recorded in SkippedCRs.
//
// Conflict resolution (same key, different values):
//   - Oldest CR by creationTimestamp wins. CRs are the only source of truth.
//   - Equal creation timestamps are resolved alphabetically by CR name.
//
// Same key, same value → recorded as Duplicate (logged, not an error).
func MergeNotificationsConfigurations(crs []v1alpha1.NotificationsConfiguration) (MergeResult, error) {
	active := filterActive(crs)
	sorted := sortByPrecedence(active)

	if len(sorted) == 0 {
		return MergeResult{Data: make(map[string]string)}, nil
	}

	var valid []v1alpha1.NotificationsConfiguration
	var skippedCRs []SkippedCR
	flat := newEntryMerger()

	for i := range sorted {
		cr := &sorted[i]
		entries, err := collectFlatEntries(cr)
		if err != nil {
			skippedCRs = append(skippedCRs, SkippedCR{Name: cr.Name, Reason: err.Error()})
			continue
		}
		valid = append(valid, sorted[i])
		for k, v := range entries {
			flat.add(k, v, cr.Name)
		}
	}

	// Merge context entries across valid CRs.
	ctx := newEntryMerger()
	for i := range valid {
		cr := &valid[i]
		if cr.Spec.Context == nil {
			continue
		}
		for k, v := range cr.Spec.Context {
			ctx.add(k, v, cr.Name)
		}
	}
	if len(ctx.data) > 0 {
		ctxData, err := serializeContext(ctx.data)
		if err != nil {
			return MergeResult{}, err
		}
		flat.data[contextConfigMapKey] = ctxData
	}

	// Merge subscription lists across valid CRs.
	subData, err := mergeSubscriptionLists(valid)
	if err != nil {
		return MergeResult{}, err
	}
	if subData != "" {
		flat.data[subscriptionsConfigMapKey] = subData
	}

	return MergeResult{
		Data:       flat.data,
		Conflicts:  append(flat.conflicts, ctx.conflicts...),
		Duplicates: append(flat.duplicates, ctx.duplicates...),
		SkippedCRs: skippedCRs,
	}, nil
}

// filterActive returns a new slice containing only CRs without a non-nil
// DeletionTimestamp. Input CRs are shallow-copied; their spec maps are not
// cloned.
func filterActive(crs []v1alpha1.NotificationsConfiguration) []v1alpha1.NotificationsConfiguration {
	var out []v1alpha1.NotificationsConfiguration
	for i := range crs {
		if crs[i].DeletionTimestamp == nil {
			out = append(out, crs[i])
		}
	}
	return out
}

// sortByPrecedence returns a new slice sorted by merge precedence:
// oldest CR first (by creationTimestamp), then alphabetically by name for ties.
// The input slice is not modified.
func sortByPrecedence(crs []v1alpha1.NotificationsConfiguration) []v1alpha1.NotificationsConfiguration {
	sorted := make([]v1alpha1.NotificationsConfiguration, len(crs))
	copy(sorted, crs)

	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CreationTimestamp.Time.Equal(sorted[j].CreationTimestamp.Time) {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].CreationTimestamp.Time.Before(sorted[j].CreationTimestamp.Time)
	})

	return sorted
}

// collectFlatEntries returns the flat ConfigMap entries contributed by one CR.
//
// Fields are processed in the same order as the existing reconciler
// (Triggers → Templates → Services → Subscriptions), so within a single CR
// later fields overwrite earlier ones for the same key, matching existing
// maps.Copy semantics.
//
// The reserved "subscriptions" key in Spec.Subscriptions is excluded because
// it is handled by YAML-list concatenation. All other Subscriptions map
// entries are included as flat entries.
//
// Returns an error if any flat-field entry uses a reserved ConfigMap key
// ("context" or "subscriptions"), or if the subscriptions YAML is malformed.
func collectFlatEntries(cr *v1alpha1.NotificationsConfiguration) (map[string]string, error) {
	merged := make(map[string]string)

	addMapEntries := func(m map[string]string, area string) error {
		if m == nil {
			return nil
		}
		for k, v := range m {
			if k == contextConfigMapKey || k == subscriptionsConfigMapKey {
				return fmt.Errorf(
					"CR %q: %s field contains reserved ConfigMap key %q which is managed by dedicated merge logic",
					cr.Name, area, k,
				)
			}
			merged[k] = v
		}
		return nil
	}

	if err := addMapEntries(cr.Spec.Triggers, "triggers"); err != nil {
		return nil, err
	}
	if err := addMapEntries(cr.Spec.Templates, "templates"); err != nil {
		return nil, err
	}
	if err := addMapEntries(cr.Spec.Services, "services"); err != nil {
		return nil, err
	}

	if cr.Spec.Subscriptions != nil {
		for k, v := range cr.Spec.Subscriptions {
			if k == subscriptionsConfigMapKey {
				continue
			}
			if k == contextConfigMapKey {
				return nil, fmt.Errorf(
					"CR %q: subscriptions field contains reserved ConfigMap key %q which is managed by dedicated merge logic",
					cr.Name, k,
				)
			}
			merged[k] = v
		}

		// Validate the "subscriptions" YAML early so invalid CRs are
		// skipped before they reach mergeSubscriptionLists.
		if raw, ok := cr.Spec.Subscriptions[subscriptionsConfigMapKey]; ok && strings.TrimSpace(raw) != "" {
			if _, err := decodeSingleDocumentList(raw, cr.Name); err != nil {
				return nil, err
			}
		}
	}

	return merged, nil
}

// serializeContext produces a deterministic YAML string from a context map.
// It uses yaml.Marshal which sorts keys alphabetically and correctly quotes
// values that YAML would otherwise interpret as booleans, numbers, or null.
func serializeContext(m map[string]string) (string, error) {
	out, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("failed to serialize merged context: %w", err)
	}
	return string(out), nil
}

// mergeSubscriptionLists parses the "subscriptions" entry from each CR's
// Spec.Subscriptions map as a YAML list, concatenates the lists in precedence
// order, and returns the serialized result.
//
// Rules:
//   - Absent or empty Subscriptions["subscriptions"] contributes nothing.
//   - Malformed YAML returns an error identifying the offending CR.
//   - A non-list top-level value returns an error identifying the offending CR.
//   - Multiple YAML documents (separated by ---) are rejected.
//   - Lists are concatenated in CR precedence order; within-CR order is
//     preserved.
//   - Duplicate list entries are preserved (no deduplication).
//
// Returns an empty string when no CR contributes subscription entries.
func mergeSubscriptionLists(sortedCRs []v1alpha1.NotificationsConfiguration) (string, error) {
	var combined []any
	for i := range sortedCRs {
		cr := &sortedCRs[i]
		if cr.Spec.Subscriptions == nil {
			continue
		}
		raw, ok := cr.Spec.Subscriptions[subscriptionsConfigMapKey]
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}

		list, err := decodeSingleDocumentList(raw, cr.Name)
		if err != nil {
			return "", err
		}
		if list == nil {
			continue
		}

		combined = append(combined, list...)
	}

	if len(combined) == 0 {
		return "", nil
	}

	out, err := yaml.Marshal(combined)
	if err != nil {
		return "", fmt.Errorf("failed to serialize merged subscriptions: %w", err)
	}

	return strings.TrimRight(string(out), "\n"), nil
}

// decodeSingleDocumentList parses raw as a single YAML document that must be a
// list. It returns nil for a null/empty document. It rejects multiple YAML
// documents, a non-list top-level value, and malformed YAML.
func decodeSingleDocumentList(raw string, crName string) ([]any, error) {
	dec := yaml.NewDecoder(bytes.NewReader([]byte(raw)))

	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf(
			"CR %q: subscriptions[%q] contains invalid YAML: %w",
			crName, subscriptionsConfigMapKey, err,
		)
	}

	if err := rejectTrailingDocuments(dec, crName); err != nil {
		return nil, err
	}

	if parsed == nil {
		return nil, nil
	}

	list, ok := parsed.([]any)
	if !ok {
		return nil, fmt.Errorf(
			"CR %q: subscriptions[%q] must be a YAML list, got %T",
			crName, subscriptionsConfigMapKey, parsed,
		)
	}

	return list, nil
}

// rejectTrailingDocuments verifies that the YAML decoder has no more documents
// to read. Ordinary trailing whitespace and comments are allowed (the decoder
// returns io.EOF for those). Additional documents — including an explicit empty
// second document (---\n) — are rejected.
//
// Tested against yaml.v2 v2.4.0: the Decoder returns ordinary errors (not
// panics) for malformed trailing content including binary garbage and invalid
// syntax after a document separator.
func rejectTrailingDocuments(dec *yaml.Decoder, crName string) error {
	var extra any
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf(
			"CR %q: subscriptions[%q] contains malformed trailing content",
			crName, subscriptionsConfigMapKey,
		)
	}
	return fmt.Errorf(
		"CR %q: subscriptions[%q] contains multiple YAML documents; "+
			"only a single document with the subscription list is allowed",
		crName, subscriptionsConfigMapKey,
	)
}
