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
	// Key is a ConfigMap data key or an individual context entry key.
	Key string

	// ExistingCR is the first CR defining the key in precedence order.
	ExistingCR string

	// ConflictingCR is the metadata.name of the CR whose value was ignored.
	ConflictingCR string

	// EqualTimestamp means the CRs have equal oldest timestamps. A stored value
	// is retained only if one of those CRs defines it.
	EqualTimestamp bool
}

// MergeDuplicate records CRs that define the same value for the same key.
type MergeDuplicate struct {
	Key         string
	FirstCR     string
	DuplicateCR string
}

// SkippedEntry records an individual key that was excluded from the merge
// because it uses a reserved ConfigMap key or has invalid content.
// The rest of the CR's entries are still merged normally.
type SkippedEntry struct {
	// Key is the entry key that was skipped.
	Key string
	// CR is the metadata.name of the CR that contained the skipped key.
	CR string
	// Reason explains why the key was skipped.
	Reason string
}

// MergeResult holds the output of MergeNotificationsConfigurations.
type MergeResult struct {
	// Data is the merged ConfigMap data; it is non-nil on success, but may be empty.
	Data map[string]string

	Conflicts []MergeConflict

	Duplicates []MergeDuplicate

	// InvalidExistingContext means stored context could not be decoded during a
	// tie, so disputed entries were omitted while other CR entries were merged.
	InvalidExistingContext bool

	// SkippedEntries records individual keys that were excluded because
	// they use a reserved key or have invalid content. The rest of the
	// CR's entries are still merged.
	SkippedEntries []SkippedEntry
}

// entryMerger keeps the first value for each key and records conflicts and
// duplicates. Callers must add CRs in precedence order.
type entryMerger struct {
	data       map[string]string
	owners     map[string]*v1alpha1.NotificationsConfiguration
	tiedValues map[string]map[string]*v1alpha1.NotificationsConfiguration
	conflicts  []MergeConflict
	duplicates []MergeDuplicate
}

func newEntryMerger() *entryMerger {
	return &entryMerger{
		data:       make(map[string]string),
		owners:     make(map[string]*v1alpha1.NotificationsConfiguration),
		tiedValues: make(map[string]map[string]*v1alpha1.NotificationsConfiguration),
	}
}

func (m *entryMerger) add(key, value string, cr *v1alpha1.NotificationsConfiguration) {
	if currentOwner, exists := m.owners[key]; exists {
		if m.data[key] == value {
			m.duplicates = append(m.duplicates, MergeDuplicate{
				Key: key, FirstCR: currentOwner.Name, DuplicateCR: cr.Name,
			})
		} else {
			equalTimestamp := currentOwner.CreationTimestamp.Equal(&cr.CreationTimestamp)
			if equalTimestamp {
				if m.tiedValues[key] == nil {
					m.tiedValues[key] = map[string]*v1alpha1.NotificationsConfiguration{m.data[key]: currentOwner}
				}
				if m.tiedValues[key][value] == nil {
					m.tiedValues[key][value] = cr
				}
			}
			m.conflicts = append(m.conflicts, MergeConflict{
				Key: key, ExistingCR: currentOwner.Name, ConflictingCR: cr.Name,
				EqualTimestamp: equalTimestamp,
			})
		}
		return
	}
	m.data[key] = value
	m.owners[key] = cr
}

// retainTiedValues keeps a disputed key only when its stored value matches an
// oldest tied CR. Run after collecting every entry so newer CRs cannot fill it.
func (m *entryMerger) retainTiedValues(existing map[string]string) {
	for key, candidates := range m.tiedValues {
		value, exists := existing[key]
		if owner := candidates[value]; exists && owner != nil {
			m.data[key] = value
			m.owners[key] = owner
		} else {
			delete(m.data, key)
			delete(m.owners, key)
		}
	}
}

// MergeNotificationsConfigurations merges all CRs in a namespace into one
// ConfigMap.Data map. Pass nil for existing when no ConfigMap exists.
//
// Merge rules:
//   - Flat fields (Triggers, Templates, Services, non-reserved Subscriptions)
//     go directly into ConfigMap.Data.
//   - Context entries are serialized into a single "context" YAML key.
//   - Subscriptions["subscriptions"] lists are concatenated across CRs.
//   - CRs with a non-nil DeletionTimestamp are excluded.
//   - Individual entries that use reserved keys are skipped and recorded
//     in SkippedEntries; the rest of the CR's entries are still merged.
//   - CRs with malformed subscriptions YAML have their subscription list
//     skipped; their other entries (triggers, templates, etc.) are still merged.
//
// Conflict resolution (same key, different values):
//   - Oldest CR by creationTimestamp wins.
//   - Conflicting values with equal oldest timestamps retain the existing value
//     only if one of the oldest tied CRs defines it; otherwise the key is omitted.
//   - If existing context cannot be decoded during a tie, disputed entries are
//     omitted while other entries are merged from the CRs.
//
// Same key, same value → recorded as Duplicate (logged, not an error).
func MergeNotificationsConfigurations(crs []v1alpha1.NotificationsConfiguration, existing map[string]string) (MergeResult, error) {
	active := filterActive(crs)
	sorted := sortByPrecedence(active)

	if len(sorted) == 0 {
		return MergeResult{Data: make(map[string]string)}, nil
	}

	flat := newEntryMerger()
	var allSkipped []SkippedEntry

	for i := range sorted {
		cr := &sorted[i]
		entries, skipped := collectFlatEntries(cr)
		allSkipped = append(allSkipped, skipped...)
		for k, v := range entries {
			flat.add(k, v, cr)
		}
	}
	flat.retainTiedValues(existing)

	// Merge context entries across all CRs.
	ctx := newEntryMerger()
	for i := range sorted {
		cr := &sorted[i]
		if cr.Spec.Context == nil {
			continue
		}
		for k, v := range cr.Spec.Context {
			ctx.add(k, v, cr)
		}
	}
	invalidContext := false
	if len(ctx.tiedValues) > 0 {
		// Decode only when needed, and retain individual context entries so
		// unrelated context updates are not blocked by a conflicting key.
		var previous map[string]string
		dec := yaml.NewDecoder(strings.NewReader(existing[contextConfigMapKey]))
		decodeErr := dec.Decode(&previous)
		var extra any
		if (decodeErr != nil && decodeErr != io.EOF) || dec.Decode(&extra) != io.EOF {
			invalidContext = true
			previous = nil
		}
		ctx.retainTiedValues(previous)
	}
	if len(ctx.data) > 0 {
		ctxData, err := serializeContext(ctx.data)
		if err != nil {
			return MergeResult{}, err
		}
		flat.data[contextConfigMapKey] = ctxData
	}

	// Merge subscription lists. CRs with malformed YAML are skipped
	// individually; their other entries remain in the merge.
	subData, subSkipped, err := mergeSubscriptionLists(sorted)
	if err != nil {
		return MergeResult{}, err
	}
	allSkipped = append(allSkipped, subSkipped...)
	if subData != "" {
		flat.data[subscriptionsConfigMapKey] = subData
	}

	return MergeResult{
		Data:                   flat.data,
		Conflicts:              append(flat.conflicts, ctx.conflicts...),
		Duplicates:             append(flat.duplicates, ctx.duplicates...),
		InvalidExistingContext: invalidContext,
		SkippedEntries:         allSkipped,
	}, nil
}

// filterActive returns a new slice of non-deleting CRs, sharing their spec maps.
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
// oldest CR first. Names only stabilize subscription ordering and reports for
// equal timestamps; conflicting key values are handled separately.
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

// collectFlatEntries collects triggers, templates, services, and extra subscription
// keys in that order; later fields overwrite earlier ones within the same CR.
// Reserved keys are skipped individually and recorded in skipped; the rest of
// the CR's entries are still returned.
func collectFlatEntries(cr *v1alpha1.NotificationsConfiguration) (entries map[string]string, skipped []SkippedEntry) {
	entries = make(map[string]string)

	addMapEntries := func(m map[string]string, area string) {
		if m == nil {
			return
		}
		for k, v := range m {
			if k == contextConfigMapKey || k == subscriptionsConfigMapKey {
				skipped = append(skipped, SkippedEntry{
					Key:    k,
					CR:     cr.Name,
					Reason: fmt.Sprintf("%s field contains reserved ConfigMap key %q which is managed by dedicated merge logic", area, k),
				})
				continue
			}
			entries[k] = v
		}
	}

	addMapEntries(cr.Spec.Triggers, "triggers")
	addMapEntries(cr.Spec.Templates, "templates")
	addMapEntries(cr.Spec.Services, "services")

	if cr.Spec.Subscriptions != nil {
		for k, v := range cr.Spec.Subscriptions {
			if k == subscriptionsConfigMapKey {
				continue
			}
			if k == contextConfigMapKey {
				skipped = append(skipped, SkippedEntry{
					Key:    k,
					CR:     cr.Name,
					Reason: fmt.Sprintf("subscriptions field contains reserved ConfigMap key %q which is managed by dedicated merge logic", k),
				})
				continue
			}
			entries[k] = v
		}
	}

	return entries, skipped
}

// serializeContext writes YAML with stable key ordering and preserves string values.
func serializeContext(m map[string]string) (string, error) {
	out, err := yaml.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("failed to serialize merged context: %w", err)
	}
	return string(out), nil
}

// mergeSubscriptionLists concatenates subscription lists in CR precedence order,
// preserving each list's order and duplicates. CRs with malformed YAML are
// skipped individually and recorded in the returned skipped slice; their other
// entries (triggers, templates, etc.) remain in the merge.
func mergeSubscriptionLists(sortedCRs []v1alpha1.NotificationsConfiguration) (string, []SkippedEntry, error) {
	var combined []any
	var skipped []SkippedEntry

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
			skipped = append(skipped, SkippedEntry{
				Key:    subscriptionsConfigMapKey,
				CR:     cr.Name,
				Reason: err.Error(),
			})
			continue
		}
		if list == nil {
			continue
		}

		combined = append(combined, list...)
	}

	if len(combined) == 0 {
		return "", skipped, nil
	}

	out, err := yaml.Marshal(combined)
	if err != nil {
		return "", nil, fmt.Errorf("failed to serialize merged subscriptions: %w", err)
	}

	return strings.TrimRight(string(out), "\n"), skipped, nil
}

// decodeSingleDocumentList accepts one YAML list or an empty/null document.
// It rejects malformed YAML, other value types, and additional documents.
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

// rejectTrailingDocuments rejects additional YAML documents or malformed
// trailing content, while allowing trailing whitespace and comments.
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
