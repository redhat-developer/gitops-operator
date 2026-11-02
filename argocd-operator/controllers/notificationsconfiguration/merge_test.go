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

// assertContextValuesAreStrings decodes the "context" ConfigMap key into
// map[string]interface{} and verifies every value is a string. This catches
// cases where yaml.Marshal failed to quote a value (e.g. "true" decoded as
// bool, "42" as int, "null" as nil).
func assertContextValuesAreStrings(t *testing.T, raw string) {
	t.Helper()
	var generic map[string]interface{}
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crOlder, crNewer})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crNewer, crOldest})
	require.NoError(t, err)

	assert.Equal(t, "OldestValue", result.Data["trigger.health"],
		"oldest CR should win regardless of alphabetical name order")
	require.Len(t, result.Conflicts, 1)
	assert.Equal(t, "zzz-custom", result.Conflicts[0].ExistingCR)
	assert.Equal(t, "aaa-custom", result.Conflicts[0].ConflictingCR)
}

func TestMerge_MultipleCRs_EqualTimestampsUseName(t *testing.T) {
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
		result, err := MergeNotificationsConfigurations(crs)
		require.NoError(t, err)
		assert.Equal(t, "A-value", result.Data["trigger.shared"])
		assert.Equal(t, "east", decodeContext(t, result.Data)["region"])
		assert.ElementsMatch(t, []MergeConflict{
			{Key: "region", ExistingCR: "cr-a", ConflictingCR: "cr-b"},
			{Key: "trigger.shared", ExistingCR: "cr-a", ConflictingCR: "cr-b"},
		}, result.Conflicts)
		assert.Equal(t, "- recipients:\n  - slack:a\n- recipients:\n  - slack:b", result.Data["subscriptions"])
	}
}

// ---------- B2. Existing-ConfigMap precedence ----------

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
		[]v1alpha1.NotificationsConfiguration{crNewer, crOlder})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB})
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

	result1, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB})
	require.NoError(t, err)
	assert.Equal(t, "A-wins", result1.Data["trigger.shared"])
	require.Len(t, result1.Conflicts, 1)

	result2, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crB})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crNewer, crOlder})
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
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
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

			result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
			require.NoError(t, err)

			raw := result.Data["context"]

			// Verify all values decode as strings via interface{}.
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
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		_, has := result.Data["context"]
		assert.False(t, has, "nil context should not produce a context key")
	})

	t.Run("empty context map", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{Context: map[string]string{}})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crNewer, crOlder})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{crA, crB})
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
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		assert.Empty(t, result.SkippedCRs)
		assert.Equal(t, cr.Spec.Triggers, result.Data)
	})

	t.Run("absent subscriptions map", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})

	t.Run("empty subscriptions value", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{"subscriptions": ""},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})

	t.Run("whitespace subscriptions value", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{"subscriptions": "   \n  "},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})

	t.Run("null YAML value", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{"subscriptions": "null"},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})

	t.Run("empty YAML list", func(t *testing.T) {
		cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{"subscriptions": "[]"},
		})
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		_, has := result.Data["subscriptions"]
		assert.False(t, has)
	})
}

func TestMerge_Subscriptions_InvalidYAML_SkipsCR(t *testing.T) {
	cr := makeCR("bad-cr", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{"subscriptions": ": [invalid yaml"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
	require.NoError(t, err)
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "bad-cr", result.SkippedCRs[0].Name)
	assert.Contains(t, result.SkippedCRs[0].Reason, "invalid YAML")
}

func TestMerge_Subscriptions_NonList_SkipsCR(t *testing.T) {
	cr := makeCR("bad-cr", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{"subscriptions": "not-a-list: true"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
	require.NoError(t, err)
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "bad-cr", result.SkippedCRs[0].Name)
	assert.Contains(t, result.SkippedCRs[0].Reason, "must be a YAML list")
}

func TestMerge_Subscriptions_ExtraMapEntriesPassThrough(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions":        "- recipients: [slack:general]",
			"defaultTriggers":      "- on-sync-status-unknown",
			"defaultTriggers.team": "- on-health-degraded",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
	require.NoError(t, err)

	assert.Contains(t, result.Data["subscriptions"], "slack:general")
	assert.Equal(t, "- on-sync-status-unknown", result.Data["defaultTriggers"])
	assert.Equal(t, "- on-health-degraded", result.Data["defaultTriggers.team"])
}

func TestMerge_Subscriptions_MultiDocument_SkipsCR(t *testing.T) {
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
				Subscriptions: map[string]string{"subscriptions": tc.raw},
			})
			result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
			require.NoError(t, err)
			require.Len(t, result.SkippedCRs, 1)
			assert.Equal(t, cr.Name, result.SkippedCRs[0].Name)
			assert.Contains(t, result.SkippedCRs[0].Reason, tc.reason)
			assert.Empty(t, result.Data)
		})
	}

	t.Run("two documents with content", func(t *testing.T) {
		cr := makeCR("multi-cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{
				"subscriptions": "- recipients: [slack:ops]\n---\n- recipients: [slack:team-a]",
			},
		})

		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		require.Len(t, result.SkippedCRs, 1)
		assert.Equal(t, "multi-cr", result.SkippedCRs[0].Name)
		assert.Contains(t, result.SkippedCRs[0].Reason, "multiple YAML documents")
	})

	t.Run("explicit empty second document", func(t *testing.T) {
		cr := makeCR("empty-doc-cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{
				"subscriptions": "- recipients: [slack:ops]\n---\n",
			},
		})

		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		require.Len(t, result.SkippedCRs, 1)
		assert.Equal(t, "empty-doc-cr", result.SkippedCRs[0].Name)
	})

	t.Run("trailing whitespace and comments are allowed", func(t *testing.T) {
		cr := makeCR("ok-cr", v1alpha1.NotificationsConfigurationSpec{
			Subscriptions: map[string]string{
				"subscriptions": "- recipients: [slack:ops]\n# a trailing comment\n\n  \n",
			},
		})

		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
		require.NoError(t, err)
		assert.Contains(t, result.Data["subscriptions"], "slack:ops")
	})
}

func TestMerge_ReservedKeyCollision_SubscriptionsInServices_SkipsCR(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"subscriptions": "should-not-be-a-service-key"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
	require.NoError(t, err)
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "cr", result.SkippedCRs[0].Name)
	assert.Contains(t, result.SkippedCRs[0].Reason, "reserved")
	assert.Contains(t, result.SkippedCRs[0].Reason, "\"subscriptions\"")
}

func TestMerge_ReservedKeyCollision_ContextInServices_SkipsCR(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"context": "should-not-be-here"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
	require.NoError(t, err)
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "cr", result.SkippedCRs[0].Name)
	assert.Contains(t, result.SkippedCRs[0].Reason, "reserved")
	assert.Contains(t, result.SkippedCRs[0].Reason, "\"context\"")
}

// ---------- F. Safety and Determinism ----------

func TestMerge_EmptyInput(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		result, err := MergeNotificationsConfigurations(nil)
		require.NoError(t, err)
		assert.NotNil(t, result.Data)
		assert.Empty(t, result.Data)
		assert.Nil(t, result.Conflicts)
	})

	t.Run("empty slice", func(t *testing.T) {
		result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{deleting, active})
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

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
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
	_, err := MergeNotificationsConfigurations(input)
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

	result1, err1 := MergeNotificationsConfigurations(order1)
	result2, err2 := MergeNotificationsConfigurations(order2)
	result3, err3 := MergeNotificationsConfigurations(order3)

	require.NoError(t, err1)
	require.NoError(t, err2)
	require.NoError(t, err3)

	assert.Equal(t, result1.Data, result2.Data)
	assert.Equal(t, result1.Data, result3.Data)
	assert.ElementsMatch(t, result1.Conflicts, result2.Conflicts)
	assert.ElementsMatch(t, result1.Conflicts, result3.Conflicts)
}

func TestMerge_ReservedKeyCollision_ContextInTriggers_SkipsCR(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"context": "should-not-be-a-trigger-key"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
	require.NoError(t, err)
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "cr", result.SkippedCRs[0].Name)
	assert.Contains(t, result.SkippedCRs[0].Reason, "reserved")
	assert.Contains(t, result.SkippedCRs[0].Reason, "\"context\"")
}

func TestMerge_ReservedKeyCollision_SubscriptionsInTemplates_SkipsCR(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Templates: map[string]string{"subscriptions": "should-not-be-a-template-key"},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
	require.NoError(t, err)
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "cr", result.SkippedCRs[0].Name)
	assert.Contains(t, result.SkippedCRs[0].Reason, "reserved")
	assert.Contains(t, result.SkippedCRs[0].Reason, "\"subscriptions\"")
}

func TestMerge_ReservedKeyCollision_ContextInSubscriptionsExtra_SkipsCR(t *testing.T) {
	cr := makeCR("cr", v1alpha1.NotificationsConfigurationSpec{
		Subscriptions: map[string]string{
			"subscriptions": "- recipients: [slack:test]",
			"context":       "should-not-be-here",
		},
	})

	result, err := MergeNotificationsConfigurations([]v1alpha1.NotificationsConfiguration{cr})
	require.NoError(t, err)
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "cr", result.SkippedCRs[0].Name)
	assert.Contains(t, result.SkippedCRs[0].Reason, "reserved")
	assert.Contains(t, result.SkippedCRs[0].Reason, "\"context\"")
}

func TestMerge_InvalidCR_SkippedButValidCRStillMerged(t *testing.T) {
	badCR := makeCR("bad-cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{
			"context":           "reserved-key-collision",
			"trigger.on-deploy": "should-be-excluded",
		},
	})
	goodCR := makeCR("good-cr", v1alpha1.NotificationsConfigurationSpec{
		Triggers: map[string]string{"trigger.on-sync": "valid-trigger"},
		Services: map[string]string{"service.slack": "token: xoxb"},
	})

	result, err := MergeNotificationsConfigurations(
		[]v1alpha1.NotificationsConfiguration{badCR, goodCR})
	require.NoError(t, err)

	// bad-cr is skipped entirely — none of its keys appear.
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "bad-cr", result.SkippedCRs[0].Name)
	assert.NotContains(t, result.Data, "trigger.on-deploy")

	// good-cr is merged normally.
	assert.Equal(t, "valid-trigger", result.Data["trigger.on-sync"])
	assert.Equal(t, "token: xoxb", result.Data["service.slack"])
}

func TestMerge_InvalidSubscriptions_SkippedButOtherCRsMerged(t *testing.T) {
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
		[]v1alpha1.NotificationsConfiguration{badCR, goodCR})
	require.NoError(t, err)

	// bad-sub-cr is skipped entirely — its trigger is also excluded.
	require.Len(t, result.SkippedCRs, 1)
	assert.Equal(t, "bad-sub-cr", result.SkippedCRs[0].Name)
	assert.NotContains(t, result.Data, "trigger.deploy")

	// good-cr is merged normally.
	assert.Equal(t, "good-trigger", result.Data["trigger.sync"])
	assert.Contains(t, result.Data["subscriptions"], "slack:ops")
}
