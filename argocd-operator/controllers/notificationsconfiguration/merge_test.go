package notificationsconfiguration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
)

// makeCR creates a CR with zero creationTimestamp.
func makeCR(name string, spec v1alpha1.NotificationsConfigurationSpec) v1alpha1.NotificationsConfiguration {
	return v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
		},
		Spec: spec,
	}
}

// makeCRAt creates a CR with an explicit creationTimestamp for precedence tests.
func makeCRAt(name string, created time.Time, spec v1alpha1.NotificationsConfigurationSpec) v1alpha1.NotificationsConfiguration {
	return v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "test-ns",
			CreationTimestamp: metav1.NewTime(created),
		},
		Spec: spec,
	}
}

// decodeContext unmarshals the "context" ConfigMap key into map[string]string.
func decodeContext(t *testing.T, data map[string]string) map[string]string {
	t.Helper()
	raw, ok := data[contextConfigMapKey]
	require.True(t, ok, "context key should be present in merged data")
	var decoded map[string]string
	require.NoError(t, yaml.Unmarshal([]byte(raw), &decoded))
	return decoded
}

// assertContextValuesAreStrings checks that YAML values such as "true", "42",
// and "null" remain strings after serialization.
func assertContextValuesAreStrings(t *testing.T, raw string) {
	t.Helper()
	var generic map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(raw), &generic))
	for k, v := range generic {
		_, ok := v.(string)
		assert.True(t, ok, "context key %q should decode as string, got %T (%v)", k, v, v)
	}
}

// ---------- A. Single CR ----------

func TestMerge_SingleCR_AllFields(t *testing.T) {
	cr := makeCR(DefaultNotificationsConfigurationName, v1alpha1.NotificationsConfigurationSpec{
		Triggers:  map[string]string{"trigger.on-created": "- when: \"true\"\n  send: [app-created]"},
		Templates: map[string]string{"template.app-created": "email:\n  subject: App created"},
		Services:  map[string]string{"service.slack": "token: $slack-token"},
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:general]\n  triggers: [on-created]",
		},
		Context: map[string]string{"env": "prod", "region": "us-east-1"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)

	assert.Equal(t, "- when: \"true\"\n  send: [app-created]", result.Data["trigger.on-created"])
	assert.Equal(t, "email:\n  subject: App created", result.Data["template.app-created"])
	assert.Equal(t, "token: $slack-token", result.Data["service.slack"])

	ctx := decodeContext(t, result.Data)
	assert.Equal(t, "prod", ctx["env"])
	assert.Equal(t, "us-east-1", ctx["region"])
	assertContextValuesAreStrings(t, result.Data["context"])

	assert.NotEmpty(t, result.Data["subscriptions"])
	assert.Empty(t, result.Conflicts)
}

func TestMerge_SingleCR_CompatibleWithExistingSemantics(t *testing.T) {
	cr := makeCR(DefaultNotificationsConfigurationName, v1alpha1.NotificationsConfigurationSpec{
		Triggers:  map[string]string{"trigger.on-created": "- when: \"true\""},
		Templates: map[string]string{"template.app-created": "email: subject: test"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)

	assert.Equal(t, "- when: \"true\"", result.Data["trigger.on-created"])
	assert.Equal(t, "email: subject: test", result.Data["template.app-created"])
	assert.Empty(t, result.Conflicts)

	_, hasContext := result.Data["context"]
	assert.False(t, hasContext, "context key should be absent when no context entries exist")

	_, hasSubs := result.Data["subscriptions"]
	assert.False(t, hasSubs, "subscriptions key should be absent when no subscription list exists")
}

func TestMerge_SingleCR_IntraCRCrossFieldOverwrite(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers:  map[string]string{"shared.key": "from-triggers"},
		Templates: map[string]string{"shared.key": "from-templates"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)

	assert.Equal(t, "from-templates", result.Data["shared.key"],
		"later field (templates) should overwrite earlier field (triggers) within the same CR")
	assert.Empty(t, result.Conflicts,
		"intra-CR cross-field overwrite is not a cross-CR conflict")
}

// ---------- B. Multiple CRs ----------

func TestMerge_MultipleCRs_DisjointKeys(t *testing.T) {
	crA := makeCR(DefaultNotificationsConfigurationName, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.a": "when: true"},
	})
	crB := makeCR("custom-b", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.b": "when: false"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB}, nil)
	require.NoError(t, err)

	assert.Equal(t, "when: true", result.Data["trigger.a"])
	assert.Equal(t, "when: false", result.Data["trigger.b"])
	assert.Empty(t, result.Conflicts)
}

func TestMerge_MultipleCRs_SameValueRecordsDuplicate(t *testing.T) {
	crA := makeCR("cr-a", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.shared": "when: true"},
	})
	crB := makeCR("cr-b", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.shared": "when: true"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB}, nil)
	require.NoError(t, err)

	assert.Equal(t, "when: true", result.Data["trigger.shared"])
	assert.Empty(t, result.Conflicts, "same value should not be a conflict")
	require.Len(t, result.Duplicates, 1, "same value should be recorded as duplicate")
	assert.Equal(t, "trigger.shared", result.Duplicates[0].Key)
	assert.Equal(t, "cr-a", result.Duplicates[0].FirstCR)
	assert.Equal(t, "cr-b", result.Duplicates[0].DuplicateCR)
}

func TestMerge_MultipleCRs_DifferentValueConflict(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	crOlder := makeCRAt("cr-older", older, v1alpha1.NotificationsConfigurationSpec{
		Templates: map[string]string{"template.sync": "OlderValue"},
	})
	crNewer := makeCRAt("cr-newer", newer, v1alpha1.NotificationsConfigurationSpec{
		Templates: map[string]string{
			"template.sync":    "NewerValue",
			"template.failure": "Failure",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crOlder, crNewer}, nil)
	require.NoError(t, err)

	assert.Equal(t, "OlderValue", result.Data["template.sync"])
	assert.Equal(t, "Failure", result.Data["template.failure"])

	require.Len(t, result.Conflicts, 1)
	assert.Equal(t, "template.sync", result.Conflicts[0].Key)
	assert.Equal(t, "cr-older", result.Conflicts[0].ExistingCR)
	assert.Equal(t, "cr-newer", result.Conflicts[0].ConflictingCR)
}

func TestMerge_MultipleCRs_OldestWinsRegardlessOfName(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	// "zzz-custom" is alphabetically last but oldest — should win.
	crOldest := makeCRAt("zzz-custom", older, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.health": "OldestValue"},
	})
	crNewer := makeCRAt("aaa-custom", newer, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.health": "NewerValue"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crNewer, crOldest}, nil)
	require.NoError(t, err)

	assert.Equal(t, "OldestValue", result.Data["trigger.health"],
		"oldest CR should win regardless of alphabetical name order")
	require.Len(t, result.Conflicts, 1)
	assert.Equal(t, "zzz-custom", result.Conflicts[0].ExistingCR)
	assert.Equal(t, "aaa-custom", result.Conflicts[0].ConflictingCR)
}

func TestMerge_MultipleCRs_EqualTimestampsOmitConflictingKeys(t *testing.T) {
	created := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	crA := makeCRAt("cr-a", created, v1alpha1.NotificationsConfigurationSpec{
		Triggers:      map[string]string{"trigger.shared": "A-value"},
		Context:       map[string]string{"region": "east"},
		Subscriptions: map[string]string{"subscriptions": "- recipients: [slack:a]"},
	})
	crB := makeCRAt("cr-b", created, v1alpha1.NotificationsConfigurationSpec{
		Triggers:      map[string]string{"trigger.shared": "B-value"},
		Context:       map[string]string{"region": "west"},
		Subscriptions: map[string]string{"subscriptions": "- recipients: [slack:b]"},
	})

	for _, crs := range [][]v1alpha1.NotificationsConfiguration{{crA, crB}, {crB, crA}} {
		result, err := MergeNotificationsConfigurations(crs, nil)
		require.NoError(t, err)
		assert.NotContains(t, result.Data, "trigger.shared")
		assert.NotContains(t, result.Data, "context")
		assert.ElementsMatch(t, []MergeConflict{
			{Key: "region", ExistingCR: "cr-a", ConflictingCR: "cr-b", EqualTimestamp: true},
			{Key: "trigger.shared", ExistingCR: "cr-a", ConflictingCR: "cr-b", EqualTimestamp: true},
		}, result.Conflicts)
		assert.Equal(t, "- recipients:\n  - slack:a\n- recipients:\n  - slack:b", result.Data["subscriptions"])
	}
}

// ---------- B2. Existing-ConfigMap precedence ----------

func TestMerge_TimestampTiesRetainOnlyMatchingValues(t *testing.T) {
	created := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	a := makeCRAt("a", created, v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"region": "a", "service.valid": "new"},
		Context:  map[string]string{"region": "east", "environment": "new"},
	})
	b := makeCRAt("b", created, v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"region": "b"},
		Context:  map[string]string{"region": "west"},
	})
	c := makeCRAt("c", created, v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"region": "c"},
		Context:  map[string]string{"region": "north"},
	})
	d := makeCRAt("d", created.Add(time.Hour), v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"region": "newer"},
		Context:  map[string]string{"region": "newer"},
	})
	duplicate := makeCRAt("duplicate", created, a.Spec)
	deleting := makeCRAt("deleting", created, v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"region": "deleted"},
		Context:  map[string]string{"region": "deleted"},
	})
	deletedAt := metav1.NewTime(created.Add(time.Hour))
	deleting.DeletionTimestamp = &deletedAt
	for _, tc := range []struct {
		name        string
		existing    map[string]string
		wantFlat    bool
		wantContext bool
	}{
		{name: "absent"},
		{name: "first CR", existing: map[string]string{"region": "a", "context": "region: east"}, wantFlat: true, wantContext: true},
		{name: "second CR", existing: map[string]string{"region": "b", "context": "region: west"}, wantFlat: true, wantContext: true},
		{name: "third CR", existing: map[string]string{"region": "c", "context": "region: north"}, wantFlat: true, wantContext: true},
		{name: "no matching CR", existing: map[string]string{"region": "stored", "context": "region: stored"}},
		{name: "only newer CR matches", existing: map[string]string{"region": "newer", "context": "region: newer"}},
		{name: "only deleting CR matches", existing: map[string]string{"region": "deleted", "context": "region: deleted"}},
		{name: "unmatched empty values", existing: map[string]string{"region": "", "context": "region: \"\""}},
		{name: "flat only", existing: map[string]string{"region": "b"}, wantFlat: true},
		{name: "context only", existing: map[string]string{"context": "region: west"}, wantContext: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, crs := range [][]v1alpha1.NotificationsConfiguration{
				{a, b, c, d, duplicate, deleting}, {deleting, duplicate, d, c, b, a}, {b, a, duplicate, d, deleting, c},
			} {
				result, err := MergeNotificationsConfigurations(crs, tc.existing)
				require.NoError(t, err)
				value, present := result.Data["region"]
				assert.Equal(t, tc.wantFlat, present)
				if tc.wantFlat {
					assert.Equal(t, tc.existing["region"], value)
				}
				context := decodeContext(t, result.Data)
				value, present = context["region"]
				assert.Equal(t, tc.wantContext, present)
				if tc.wantContext {
					assert.Equal(t, decodeContext(t, tc.existing)["region"], value)
				}
				assert.Equal(t, "new", result.Data["service.valid"])
				assert.Equal(t, "new", context["environment"])
			}
		})
	}

	// Equal values and conflicts between newer CRs do not override an older winner.
	for _, crs := range [][]v1alpha1.NotificationsConfiguration{
		{a, duplicate},
		{makeCRAt("oldest", created.Add(-time.Hour), a.Spec), a, b},
	} {
		result, err := MergeNotificationsConfigurations(crs, map[string]string{"region": "stored"})
		require.NoError(t, err)
		assert.Equal(t, "a", result.Data["region"])
		for _, conflict := range result.Conflicts {
			assert.False(t, conflict.EqualTimestamp)
		}
	}
}

func TestMerge_TimestampTieMatchingEmptyValue(t *testing.T) {
	a := makeCR("a", v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"service.shared": "a"},
		Context:  map[string]string{"region": "east"},
	})
	b := makeCR("b", v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"service.shared": ""},
		Context:  map[string]string{"region": ""},
	})
	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{a, b}, map[string]string{
		"service.shared": "", "context": "region: \"\"",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Data, "service.shared")
	assert.Empty(t, result.Data["service.shared"])
	assert.Equal(t, map[string]string{"region": ""}, decodeContext(t, result.Data))

	result, err = MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{a, b}, nil)
	require.NoError(t, err)
	assert.NotContains(t, result.Data, "service.shared")
	assert.NotContains(t, result.Data, "context")
}

func TestMerge_TiedContextOmitsUnreadableValue(t *testing.T) {
	a := makeCR("a", v1alpha1.NotificationsConfigurationSpec{
		Context:       map[string]string{"region": "east", "environment": "new"},
		Triggers:      map[string]string{"trigger.valid": "new-trigger"},
		Templates:     map[string]string{"template.valid": "new-template"},
		Services:      map[string]string{"service.valid": "new-service"},
		Subscriptions: map[string]string{"subscriptions": "- recipients: [slack:ops]"},
	})
	b := makeCR("b", v1alpha1.NotificationsConfigurationSpec{Context: map[string]string{"region": "west"}})
	for _, raw := range []string{
		"region: [secret-value]",
		"region: east\nunrelated: [secret-value]",
		"region: [unterminated",
		"region: old\n---\nregion: other",
		"null\n---\nregion: other",
		"region: old\n---\n",
	} {
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{a, b}, map[string]string{"context": raw})
		require.NoError(t, err)
		assert.True(t, result.InvalidExistingContext)
		assert.Equal(t, map[string]string{"environment": "new"}, decodeContext(t, result.Data))
		assert.Equal(t, "new-trigger", result.Data["trigger.valid"])
		assert.Equal(t, "new-template", result.Data["template.valid"])
		assert.Equal(t, "new-service", result.Data["service.valid"])
		assert.Contains(t, result.Data["subscriptions"], "slack:ops")
		require.Len(t, result.Conflicts, 1)
		assert.True(t, result.Conflicts[0].EqualTimestamp)
	}

	for _, raw := range []string{"", "# comment only\n", "null", "{}"} {
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{a, b}, map[string]string{"context": raw})
		require.NoError(t, err)
		assert.False(t, result.InvalidExistingContext)
		assert.Equal(t, map[string]string{"environment": "new"}, decodeContext(t, result.Data))
	}
}

func TestMerge_OldestCR_AlwaysWins_RegardlessOfConfigMap(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	crOlder := makeCRAt("cr-older", older, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.x": "older-val"},
	})
	crNewer := makeCRAt("cr-newer", newer, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.x": "newer-val"},
	})

	result, err := MergeNotificationsConfigurations(
		[]v1alpha1.NotificationsConfiguration{crNewer, crOlder}, nil)
	require.NoError(t, err)

	assert.Equal(t, "older-val", result.Data["trigger.x"],
		"oldest CR always wins, CRs are the only source of truth")
	require.Len(t, result.Conflicts, 1)
	assert.Equal(t, "cr-older", result.Conflicts[0].ExistingCR)
	assert.Equal(t, "cr-newer", result.Conflicts[0].ConflictingCR)
}

func TestMerge_MultipleCRs_ConflictingCRNonConflictingKeysRetained(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	crA := makeCRAt("cr-a", older, v1alpha1.NotificationsConfigurationSpec{
		Triggers:  map[string]string{"trigger.shared": "A-value"},
		Templates: map[string]string{"template.a-only": "A-template"},
	})
	crB := makeCRAt("cr-b", newer, v1alpha1.NotificationsConfigurationSpec{
		Triggers:  map[string]string{"trigger.shared": "B-value"},
		Templates: map[string]string{"template.b-only": "B-template"},
		Services:  map[string]string{"service.webhook": "url: http://example.com"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB}, nil)
	require.NoError(t, err)

	assert.Equal(t, "A-value", result.Data["trigger.shared"])
	assert.Equal(t, "A-template", result.Data["template.a-only"])
	assert.Equal(t, "B-template", result.Data["template.b-only"])
	assert.Equal(t, "url: http://example.com", result.Data["service.webhook"])
}

func TestMerge_MultipleCRs_CrossFieldConflictAcrossCRs(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	crA := makeCRAt("cr-a", older, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"defaultTriggers": "A-value"},
	})
	crB := makeCRAt("cr-b", newer, v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"defaultTriggers": "B-value",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB}, nil)
	require.NoError(t, err)

	assert.Equal(t, "A-value", result.Data["defaultTriggers"])
	require.Len(t, result.Conflicts, 1)
	assert.Equal(t, "cr-a", result.Conflicts[0].ExistingCR)
}

// ---------- C. ExistingCR Removal ----------

func TestMerge_ExistingCRRemoval(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	crA := makeCRAt("cr-a", older, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.shared": "A-wins"},
	})
	crB := makeCRAt("cr-b", newer, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.shared": "B-value"},
	})

	result1, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB}, nil)
	require.NoError(t, err)
	assert.Equal(t, "A-wins", result1.Data["trigger.shared"])
	require.Len(t, result1.Conflicts, 1)

	result2, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crB}, nil)
	require.NoError(t, err)
	assert.Equal(t, "B-value", result2.Data["trigger.shared"])
	assert.Empty(t, result2.Conflicts)
}

// ---------- D. Context ----------

func TestMerge_Context_DistinctEntries(t *testing.T) {
	crA := makeCR("cr-a", v1alpha1.NotificationsConfigurationSpec{
		Context: map[string]string{"env": "prod"},
	})
	crB := makeCR("cr-b", v1alpha1.NotificationsConfigurationSpec{
		Context: map[string]string{"region": "us-east-1"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB}, nil)
	require.NoError(t, err)

	ctx := decodeContext(t, result.Data)
	assert.Equal(t, "prod", ctx["env"])
	assert.Equal(t, "us-east-1", ctx["region"])
	assert.Empty(t, result.Conflicts)
}

func TestMerge_Context_OldestCRWins(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	crOlder := makeCRAt("cr-older", older, v1alpha1.NotificationsConfigurationSpec{
		Context: map[string]string{"env": "production"},
	})
	crNewer := makeCRAt("cr-newer", newer, v1alpha1.NotificationsConfigurationSpec{
		Context: map[string]string{"env": "staging"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crNewer, crOlder}, nil)
	require.NoError(t, err)

	ctx := decodeContext(t, result.Data)
	assert.Equal(t, "production", ctx["env"])

	require.Len(t, result.Conflicts, 1)
	assert.Equal(t, "env", result.Conflicts[0].Key)
	assert.Equal(t, "cr-older", result.Conflicts[0].ExistingCR)
	assert.Equal(t, "cr-newer", result.Conflicts[0].ConflictingCR)
}

func TestMerge_Context_SerializationStable(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Context: map[string]string{"z": "last", "a": "first", "m": "middle"},
	})

	var prev string
	for range 10 {
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)

		if prev == "" {
			prev = result.Data["context"]
		} else {
			assert.Equal(t, prev, result.Data["context"], "context serialization must be stable across runs")
		}
	}

	ctx := decodeContext(t, map[string]string{"context": prev})
	assert.Equal(t, map[string]string{"a": "first", "m": "middle", "z": "last"}, ctx)
}

func TestMerge_Context_RoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"multiline no trailing newline", "msg", "line1\nline2"},
		{"multiline with trailing newline", "msg", "line1\nline2\n"},
		{"colon in value", "status", "Status: failed"},
		{"hash in value", "note", "Hello #team"},
		{"boolean true", "flag", "true"},
		{"boolean false", "flag", "false"},
		{"boolean yes", "flag", "yes"},
		{"null string", "val", "null"},
		{"octal-like number", "code", "0123"},
		{"empty string value", "empty", ""},
		{"leading and trailing spaces", "spaced", "  hello  "},
		{"single quotes in value", "q", "it's a test"},
		{"double quotes in value", "q", "say \"hello\""},
		{"unicode text", "greeting", "こんにちは 🎉"},
		{"key requiring quoting", "key: with colon", "value"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
				Context: map[string]string{tc.key: tc.value},
			})

			result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
			require.NoError(t, err)

			raw := result.Data["context"]

			// Verify all values decode as strings via any.
			assertContextValuesAreStrings(t, raw)

			// Verify exact round-trip into map[string]string.
			ctx := decodeContext(t, result.Data)
			assert.Equal(t, tc.value, ctx[tc.key],
				"round-trip mismatch for key %q", tc.key)
		})
	}
}

// TestMerge_Context_AbsentNilEmpty documents the policy for absent, nil, and
// empty context maps: the "context" key is omitted from the merged data.
func TestMerge_Context_AbsentNilEmpty(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{Context: nil})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		_, has := result.Data["context"]
		assert.False(t, has, "nil context should not produce a context key")
	})

	t.Run("empty context map", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{Context: map[string]string{}})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		_, has := result.Data["context"]
		assert.False(t, has, "empty context map should not produce a context key")
	})
}

// ---------- E. Subscriptions ----------

func TestMerge_Subscriptions_TwoCRsConcatenated(t *testing.T) {
	crA := makeCR(DefaultNotificationsConfigurationName, v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:team-a]\n  triggers: [on-sync-failed]",
		},
	})
	crB := makeCR("custom", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:team-b]\n  triggers: [on-health-degraded]",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Data["subscriptions"], "slack:team-a")
	assert.Contains(t, result.Data["subscriptions"], "slack:team-b")
}

func TestMerge_Subscriptions_OrderFollowsPrecedence(t *testing.T) {
	older := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	crOlder := makeCRAt("cr-older", older, v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:older-team]",
		},
	})
	crNewer := makeCRAt("cr-newer", newer, v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:newer-team]",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crNewer, crOlder}, nil)
	require.NoError(t, err)

	sub := result.Data["subscriptions"]
	olderIdx := strings.Index(sub, "slack:older-team")
	newerIdx := strings.Index(sub, "slack:newer-team")
	require.NotEqual(t, -1, olderIdx, "older CR's entry should be present")
	require.NotEqual(t, -1, newerIdx, "newer CR's entry should be present")
	assert.Less(t, olderIdx, newerIdx,
		"older CR's subscriptions should appear before newer CR's subscriptions")
}

func TestMerge_Subscriptions_WithinCROrderPreserved(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:first]\n- recipients: [slack:second]\n- recipients: [slack:third]",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)

	sub := result.Data["subscriptions"]
	firstIdx := strings.Index(sub, "slack:first")
	secondIdx := strings.Index(sub, "slack:second")
	thirdIdx := strings.Index(sub, "slack:third")
	assert.Less(t, firstIdx, secondIdx)
	assert.Less(t, secondIdx, thirdIdx)
}

func TestMerge_Subscriptions_DuplicatesPreserved(t *testing.T) {
	crA := makeCR("cr-a", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:same-channel]",
		},
	})
	crB := makeCR("cr-b", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:same-channel]",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB}, nil)
	require.NoError(t, err)

	count := strings.Count(result.Data["subscriptions"], "slack:same-channel")
	assert.Equal(t, 2, count, "duplicate subscription entries must be preserved")
}

func TestMerge_Subscriptions_EmptyInput(t *testing.T) {
	t.Run("comment-only subscriptions preserve other configuration", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Triggers:      map[string]string{"trigger.keep": "when: true"},
			Subscriptions: map[string]string{"subscriptions": "# add subscriptions later\n"},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		assert.Empty(t, result.SkippedEntries)
		assert.Equal(t, cr.Spec.Triggers, result.Data)
	})

	t.Run("absent subscriptions map", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})

	t.Run("empty subscriptions value", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{"subscriptions": ""},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})

	t.Run("whitespace subscriptions value", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{"subscriptions": "   \n  "},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})

	t.Run("null YAML value", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{"subscriptions": "null"},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})

	t.Run("empty YAML list", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{"subscriptions": "[]"},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})
}

func TestMerge_Subscriptions_InvalidYAML_SkipsOnlySubscriptions(t *testing.T) {
	cr := makeCR("bad-cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers:      map[string]string{"trigger.keep": "when: true"},
		Subscriptions: map[string]string{"subscriptions": ": [invalid yaml"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "bad-cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "subscriptions", result.SkippedEntries[0].Key)
	assert.Contains(t, result.SkippedEntries[0].Reason, "invalid YAML")
	// Other entries from the same CR are still merged.
	assert.Equal(t, "when: true", result.Data["trigger.keep"])
}

func TestMerge_Subscriptions_NonList_SkipsOnlySubscriptions(t *testing.T) {
	cr := makeCR("bad-cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers:      map[string]string{"trigger.keep": "val"},
		Subscriptions: map[string]string{"subscriptions": "not-a-list: true"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "bad-cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "subscriptions", result.SkippedEntries[0].Key)
	assert.Contains(t, result.SkippedEntries[0].Reason, "must be a YAML list")
	assert.Equal(t, "val", result.Data["trigger.keep"])
}

func TestMerge_Subscriptions_ExtraMapEntriesPassThrough(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions":        "- recipients: [slack:general]",
			"defaultTriggers":      "- on-sync-status-unknown",
			"defaultTriggers.team": "- on-health-degraded",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)

	assert.Contains(t, result.Data["subscriptions"], "slack:general")
	assert.Equal(t, "- on-sync-status-unknown", result.Data["defaultTriggers"])
	assert.Equal(t, "- on-health-degraded", result.Data["defaultTriggers.team"])
}

func TestMerge_Subscriptions_MultiDocument_SkipsOnlySubscriptions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		raw    string
		reason string
	}{
		{"null first document", "null\n---\n- recipients: [slack:ops]", "multiple YAML documents"},
		{"empty first document", "---\n---\n- recipients: [slack:ops]", "multiple YAML documents"},
		{"malformed trailing document", "null\n---\n: [bad", "malformed trailing content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := makeCR("multi-cr", v1alpha1.NotificationsConfigurationSpec{
				Triggers:      map[string]string{"trigger.keep": "val"},
				Subscriptions: map[string]string{"subscriptions": tc.raw},
			})
			result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
			require.NoError(t, err)
			require.Len(t, result.SkippedEntries, 1)
			assert.Equal(t, cr.Name, result.SkippedEntries[0].CR)
			assert.Equal(t, "subscriptions", result.SkippedEntries[0].Key)
			assert.Contains(t, result.SkippedEntries[0].Reason, tc.reason)
			// Other entries from the same CR are still merged.
			assert.Equal(t, "val", result.Data["trigger.keep"])
		})
	}

	t.Run("two documents with content", func(t *testing.T) {
		cr := makeCR("multi-cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{
				"subscriptions": "- recipients: [slack:ops]\n---\n- recipients: [slack:team-a]",
			},
		})

		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		require.Len(t, result.SkippedEntries, 1)
		assert.Equal(t, "multi-cr", result.SkippedEntries[0].CR)
		assert.Contains(t, result.SkippedEntries[0].Reason, "multiple YAML documents")
	})

	t.Run("explicit empty second document", func(t *testing.T) {
		cr := makeCR("empty-doc-cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{
				"subscriptions": "- recipients: [slack:ops]\n---\n",
			},
		})

		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		require.Len(t, result.SkippedEntries, 1)
		assert.Equal(t, "empty-doc-cr", result.SkippedEntries[0].CR)
	})

	t.Run("trailing whitespace and comments are allowed", func(t *testing.T) {
		cr := makeCR("ok-cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{
				"subscriptions": "- recipients: [slack:ops]\n# a trailing comment\n\n  \n",
			},
		})

		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
		require.NoError(t, err)
		assert.Contains(t, result.Data["subscriptions"], "slack:ops")
	})
}

func TestMerge_ReservedKeyCollision_SubscriptionsInServices_SkipsKey(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{
			"subscriptions":  "should-not-be-a-service-key",
			"service.keeper": "kept",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "subscriptions", result.SkippedEntries[0].Key)
	assert.Contains(t, result.SkippedEntries[0].Reason, "reserved")
	assert.Equal(t, "kept", result.Data["service.keeper"])
}

func TestMerge_ReservedKeyCollision_ContextInServices_SkipsKey(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{
			"context":        "should-not-be-here",
			"service.keeper": "kept",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "context", result.SkippedEntries[0].Key)
	assert.Contains(t, result.SkippedEntries[0].Reason, "reserved")
	assert.Equal(t, "kept", result.Data["service.keeper"])
}

// ---------- F. Safety and Determinism ----------

func TestMerge_EmptyInput(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		result, err := MergeNotificationsConfigurations(nil, nil)
		require.NoError(t, err)
		assert.NotNil(t, result.Data)
		assert.Empty(t, result.Data)
		assert.Nil(t, result.Conflicts)
	})

	t.Run("empty slice", func(t *testing.T) {
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{}, nil)
		require.NoError(t, err)
		assert.NotNil(t, result.Data)
		assert.Empty(t, result.Data)
		assert.Nil(t, result.Conflicts)
	})
}

func TestMerge_NilAndEmptyMaps(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers:      nil,
		Templates:     map[string]string{},
		Services:      nil,
		Subscriptions: map[string]string{},
		Context:       nil,
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Data)
	assert.Nil(t, result.Conflicts)
}

func TestMerge_CRsMarkedForDeletion(t *testing.T) {
	now := metav1.NewTime(time.Now())
	deleting := makeCR("deleting", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.x": "should-not-appear"},
	})
	deleting.DeletionTimestamp = &now

	active := makeCR("active", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.y": "should-appear"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{deleting, active}, nil)
	require.NoError(t, err)

	_, hasX := result.Data["trigger.x"]
	assert.False(t, hasX, "deleted CR's keys must not appear")
	assert.Equal(t, "should-appear", result.Data["trigger.y"])
}

func TestMerge_AllCRsDeleted(t *testing.T) {
	now := metav1.NewTime(time.Now())
	cr := makeCR("gone", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.x": "val"},
	})
	cr.DeletionTimestamp = &now

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Data)
	assert.Nil(t, result.Conflicts)
}

func TestMerge_InputNotMutated(t *testing.T) {
	triggers := map[string]string{"trigger.a": "val-a"}
	context := map[string]string{"env": "prod"}
	subscriptions := map[string]string{
		"subscriptions": "- recipients: [slack:test]",
		"extra":         "extra-val",
	}

	cr := v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "cr", Namespace: "ns"},
		Spec: v1alpha1.NotificationsConfigurationSpec{
			Triggers:      triggers,
			Context:       context,
			Subscriptions: subscriptions,
		},
	}

	input := []v1alpha1.NotificationsConfiguration{cr}
	_, err := MergeNotificationsConfigurations(input, nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{"trigger.a": "val-a"}, triggers)
	assert.Equal(t, map[string]string{"env": "prod"}, context)
	assert.Equal(t, map[string]string{
		"subscriptions": "- recipients: [slack:test]",
		"extra":         "extra-val",
	}, subscriptions)
	assert.Equal(t, "cr", input[0].Name)
}

func TestMerge_ReorderedInputSameResult(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	crA := makeCRAt("cr-oldest", t1, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.shared": "Default", "trigger.default-only": "D"},
		Context:  map[string]string{"env": "prod"},
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:default]",
		},
	})
	crB := makeCRAt("cr-middle", t2, v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.shared": "Beta", "trigger.beta-only": "B"},
		Context:  map[string]string{"env": "staging", "region": "eu"},
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:beta]",
		},
	})
	crC := makeCRAt("cr-newest", t3, v1alpha1.NotificationsConfigurationSpec{
		Templates: map[string]string{"template.alpha": "A"},
	})

	order1 := []v1alpha1.NotificationsConfiguration{crA, crB, crC}
	order2 := []v1alpha1.NotificationsConfiguration{crC, crA, crB}
	order3 := []v1alpha1.NotificationsConfiguration{crB, crC, crA}

	result1, err1 := MergeNotificationsConfigurations(order1, nil)
	result2, err2 := MergeNotificationsConfigurations(order2, nil)
	result3, err3 := MergeNotificationsConfigurations(order3, nil)

	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NoError(t, err3)

	assert.Equal(t, result1.Data, result2.Data)
	assert.Equal(t, result1.Data, result3.Data)
	assert.ElementsMatch(t, result1.Conflicts, result2.Conflicts)
	assert.ElementsMatch(t, result1.Conflicts, result3.Conflicts)
}

func TestMerge_ReservedKeyCollision_ContextInTriggers_SkipsKey(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{
			"context":      "should-not-be-a-trigger-key",
			"trigger.keep": "kept",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "context", result.SkippedEntries[0].Key)
	assert.Contains(t, result.SkippedEntries[0].Reason, "reserved")
	assert.Equal(t, "kept", result.Data["trigger.keep"])
}

func TestMerge_ReservedKeyCollision_SubscriptionsInTemplates_SkipsKey(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Templates: map[string]string{
			"subscriptions": "should-not-be-a-template-key",
			"template.keep": "kept",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "subscriptions", result.SkippedEntries[0].Key)
	assert.Contains(t, result.SkippedEntries[0].Reason, "reserved")
	assert.Equal(t, "kept", result.Data["template.keep"])
}

func TestMerge_ReservedKeyCollision_ContextInSubscriptionsExtra_SkipsKey(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:test]",
			"context":       "should-not-be-here",
			"extra.keep":    "kept",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr}, nil)
	require.NoError(t, err)
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "context", result.SkippedEntries[0].Key)
	assert.Contains(t, result.SkippedEntries[0].Reason, "reserved")
	// The valid entries are still merged.
	assert.Equal(t, "kept", result.Data["extra.keep"])
	assert.Contains(t, result.Data["subscriptions"], "slack:test")
}

func TestMerge_ReservedKey_OnlyBadKeySkipped_OtherEntriesKept(t *testing.T) {
	badCR := makeCR("bad-cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{
			"context":           "reserved-key-collision",
			"trigger.on-deploy": "should-be-kept",
		},
	})
	goodCR := makeCR("good-cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.on-sync": "valid-trigger"},
		Services: map[string]string{"service.slack": "token: xoxb"},
	})

	result, err := MergeNotificationsConfigurations(
		[]v1alpha1.NotificationsConfiguration{badCR, goodCR}, nil)
	require.NoError(t, err)

	// Only the reserved key is skipped.
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "bad-cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "context", result.SkippedEntries[0].Key)

	// bad-cr's valid trigger IS kept.
	assert.Equal(t, "should-be-kept", result.Data["trigger.on-deploy"])

	// good-cr is merged normally.
	assert.Equal(t, "valid-trigger", result.Data["trigger.on-sync"])
	assert.Equal(t, "token: xoxb", result.Data["service.slack"])
}

func TestMerge_InvalidSubscriptions_OnlySubsSkipped_OtherEntriesKept(t *testing.T) {
	badCR := makeCR("bad-sub-cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.deploy": "bad-cr-trigger"},
		Subscriptions: map[string]string{
			"subscriptions": "not-a-list: true",
		},
	})
	goodCR := makeCR("good-cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.sync": "good-trigger"},
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:ops]",
		},
	})

	result, err := MergeNotificationsConfigurations(
		[]v1alpha1.NotificationsConfiguration{badCR, goodCR}, nil)
	require.NoError(t, err)

	// Only the subscriptions entry from bad-sub-cr is skipped.
	require.Len(t, result.SkippedEntries, 1)
	assert.Equal(t, "bad-sub-cr", result.SkippedEntries[0].CR)
	assert.Equal(t, "subscriptions", result.SkippedEntries[0].Key)

	// bad-sub-cr's trigger IS kept.
	assert.Equal(t, "bad-cr-trigger", result.Data["trigger.deploy"])

	// good-cr is merged normally.
	assert.Equal(t, "good-trigger", result.Data["trigger.sync"])
	assert.Contains(t, result.Data["subscriptions"], "slack:ops")
}
