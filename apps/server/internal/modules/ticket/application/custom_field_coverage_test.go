package application

import (
	"context"
	"testing"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/ticket/domain"
)

func f64Ptr(v float64) *float64 { return &v }
func intPtrCf(v int) *int       { return &v }

func TestCoverageValidateBranches(t *testing.T) {
	validator := NewCustomFieldValidator()
	ctx := map[string]interface{}{"ticket.priority": "high"}

	// provided empty -> nil, nil
	values, err := validator.Validate([]CustomFieldDefinition{{ID: 1, Key: "a", Type: "string", Active: true}}, nil, ctx, false)
	if err != nil || values != nil {
		t.Fatalf("expected nil values, got %+v %v", values, err)
	}

	// unknown key
	_, err = validator.Validate([]CustomFieldDefinition{}, map[string]interface{}{"nope": "x"}, ctx, false)
	if err == nil || err.Error() != "unknown custom field: nope" {
		t.Fatalf("expected unknown field error, got %v", err)
	}

	// inactive field skipped
	values, err = validator.Validate(
		[]CustomFieldDefinition{{ID: 1, Key: "a", Type: "string", Active: false}},
		map[string]interface{}{"a": "x"}, ctx, false)
	if err != nil || len(values) != 0 {
		t.Fatalf("expected inactive field skipped, got %+v %v", values, err)
	}

	// empty value skipped
	values, err = validator.Validate(
		[]CustomFieldDefinition{{ID: 1, Key: "a", Type: "string", Active: true}},
		map[string]interface{}{"a": "   "}, ctx, false)
	if err != nil || len(values) != 0 {
		t.Fatalf("expected empty value skipped, got %+v %v", values, err)
	}

	// hidden field skipped
	values, err = validator.Validate(
		[]CustomFieldDefinition{{ID: 1, Key: "a", Type: "string", Active: true, ShowWhen: &CustomFieldCondition{All: []CustomFieldClause{{Field: "ticket.priority", Op: "eq", Value: "low"}}}}},
		map[string]interface{}{"a": "x"}, ctx, false)
	if err != nil || len(values) != 0 {
		t.Fatalf("expected hidden field skipped, got %+v %v", values, err)
	}

	// normalize error wrapped
	_, err = validator.Validate(
		[]CustomFieldDefinition{{ID: 1, Key: "a", Type: "string", Active: true}},
		map[string]interface{}{"a": 42}, ctx, false)
	if err == nil || err.Error() != `custom field "a": expected string` {
		t.Fatalf("expected normalize error, got %v", err)
	}

	// required hidden field skipped even when enforceRequired
	values, err = validator.Validate(
		[]CustomFieldDefinition{{ID: 1, Key: "a", Type: "string", Active: true, Required: true,
			ShowWhen: &CustomFieldCondition{All: []CustomFieldClause{{Field: "ticket.priority", Op: "eq", Value: "low"}}}}},
		map[string]interface{}{}, ctx, true)
	if err != nil || values != nil {
		t.Fatalf("expected hidden required skip, got %+v %v", values, err)
	}

	// required missing
	_, err = validator.Validate(
		[]CustomFieldDefinition{{ID: 1, Key: "a", Type: "string", Active: true, Required: true}},
		map[string]interface{}{}, ctx, true)
	if err == nil {
		t.Fatal("expected required error")
	}

	// required but empty value
	_, err = validator.Validate(
		[]CustomFieldDefinition{{ID: 1, Key: "a", Type: "string", Active: true, Required: true}},
		map[string]interface{}{"a": ""}, ctx, true)
	if err == nil {
		t.Fatal("expected required error for empty value")
	}

	// happy path mapping
	values, err = validator.Validate(
		[]CustomFieldDefinition{{ID: 3, Key: "env", Type: "select", Active: true, Options: []string{"prod"}}},
		map[string]interface{}{"env": "prod"}, ctx, false)
	if err != nil || len(values) != 1 || values[0].CustomFieldID != 3 || values[0].Value != "prod" {
		t.Fatalf("unexpected values: %+v %v", values, err)
	}
}

func TestCoverageCustomFieldConditionMet(t *testing.T) {
	ctx := map[string]interface{}{"p": "high", "e": "prod"}
	if !customFieldConditionMet(nil, ctx) {
		t.Fatal("nil condition should be met")
	}
	if !customFieldConditionMet(&CustomFieldCondition{}, ctx) {
		t.Fatal("empty condition should be met")
	}
	all := &CustomFieldCondition{All: []CustomFieldClause{{Field: "p", Op: "eq", Value: "high"}, {Field: "e", Op: "eq", Value: "prod"}}}
	if !customFieldConditionMet(all, ctx) {
		t.Fatal("all clauses matched expected")
	}
	allFalse := &CustomFieldCondition{All: []CustomFieldClause{{Field: "p", Op: "eq", Value: "low"}}}
	if customFieldConditionMet(allFalse, ctx) {
		t.Fatal("unmet all clause should not be met")
	}
	anyHit := &CustomFieldCondition{Any: []CustomFieldClause{{Field: "p", Op: "eq", Value: "low"}, {Field: "e", Op: "eq", Value: "prod"}}}
	if !customFieldConditionMet(anyHit, ctx) {
		t.Fatal("any clause matched expected")
	}
	anyMiss := &CustomFieldCondition{Any: []CustomFieldClause{{Field: "p", Op: "eq", Value: "low"}}}
	if customFieldConditionMet(anyMiss, ctx) {
		t.Fatal("unmet any clauses should not be met")
	}
}

func TestCoverageEvalCustomFieldClause(t *testing.T) {
	ctx := map[string]interface{}{"p": "high"}
	if evalCustomFieldClause(CustomFieldClause{Field: "missing"}, ctx) {
		t.Fatal("missing field should be false")
	}
	cases := []struct {
		op    string
		value interface{}
		want  bool
	}{
		{"", "high", true},
		{"eq", "high", true},
		{"eq", "low", false},
		{"ne", "low", true},
		{"ne", "high", false},
		{"in", []interface{}{"high", "urgent"}, true},
		{"in", []interface{}{"low"}, false},
		{"not_in", []interface{}{"low"}, true},
		{"not_in", []interface{}{"high"}, false},
		{"bogus", "high", false},
	}
	for _, tc := range cases {
		if got := evalCustomFieldClause(CustomFieldClause{Field: "p", Op: tc.op, Value: tc.value}, ctx); got != tc.want {
			t.Fatalf("op %q value %v: expected %v, got %v", tc.op, tc.value, tc.want, got)
		}
	}
}

func TestCoverageNormalizeCustomFieldValue(t *testing.T) {
	minL, maxL := 2, 5
	strDef := CustomFieldDefinition{Type: "string", Validation: CustomFieldValidation{MinLength: &minL, MaxLength: &maxL}}

	if v, err := normalizeCustomFieldValue(strDef, "  ab  "); err != nil || v != "ab" {
		t.Fatalf("string normalize failed: %q %v", v, err)
	}
	if _, err := normalizeCustomFieldValue(strDef, "a"); err == nil {
		t.Fatal("expected min length error")
	}
	if _, err := normalizeCustomFieldValue(strDef, "abcdef"); err == nil {
		t.Fatal("expected max length error")
	}
	if _, err := normalizeCustomFieldValue(CustomFieldDefinition{Type: "string"}, 5); err == nil {
		t.Fatal("expected string type error")
	}

	numDef := CustomFieldDefinition{Type: "number", Validation: CustomFieldValidation{Min: f64Ptr(1), Max: f64Ptr(10)}}
	if v, err := normalizeCustomFieldValue(numDef, " 3.5 "); err != nil || v != "3.5" {
		t.Fatalf("number normalize failed: %q %v", v, err)
	}
	if _, err := normalizeCustomFieldValue(numDef, 0.5); err == nil {
		t.Fatal("expected min error")
	}
	if _, err := normalizeCustomFieldValue(numDef, 11); err == nil {
		t.Fatal("expected max error")
	}
	if _, err := normalizeCustomFieldValue(CustomFieldDefinition{Type: "number"}, "abc"); err == nil {
		t.Fatal("expected invalid number error")
	}

	boolDef := CustomFieldDefinition{Type: "boolean"}
	if v, err := normalizeCustomFieldValue(boolDef, true); err != nil || v != "true" {
		t.Fatalf("bool true failed: %q %v", v, err)
	}
	if v, err := normalizeCustomFieldValue(boolDef, "NO"); err != nil || v != "false" {
		t.Fatalf("bool false failed: %q %v", v, err)
	}
	if _, err := normalizeCustomFieldValue(boolDef, "maybe"); err == nil {
		t.Fatal("expected invalid boolean error")
	}
	if _, err := normalizeCustomFieldValue(boolDef, 1); err == nil {
		t.Fatal("expected invalid boolean type error")
	}

	dateDef := CustomFieldDefinition{Type: "date"}
	if v, err := normalizeCustomFieldValue(dateDef, " 2024-05-06 "); err != nil || v != "2024-05-06" {
		t.Fatalf("date normalize failed: %q %v", v, err)
	}
	if v, err := normalizeCustomFieldValue(dateDef, "2024-05-06T10:00:00Z"); err != nil || v != "2024-05-06" {
		t.Fatalf("rfc3339 date failed: %q %v", v, err)
	}
	if v, err := normalizeCustomFieldValue(dateDef, ""); err != nil || v != "" {
		t.Fatalf("empty date failed: %q %v", v, err)
	}
	if _, err := normalizeCustomFieldValue(dateDef, "not-a-date"); err == nil {
		t.Fatal("expected invalid date error")
	}
	if _, err := normalizeCustomFieldValue(dateDef, 123); err == nil {
		t.Fatal("expected date type error")
	}

	selDef := CustomFieldDefinition{Type: "select", Options: []string{"a", "b"}}
	if v, err := normalizeCustomFieldValue(selDef, " a "); err != nil || v != "a" {
		t.Fatalf("select normalize failed: %q %v", v, err)
	}
	if _, err := normalizeCustomFieldValue(selDef, "c"); err == nil {
		t.Fatal("expected invalid option error")
	}
	if _, err := normalizeCustomFieldValue(selDef, 7); err == nil {
		t.Fatal("expected select type error")
	}

	multiDef := CustomFieldDefinition{Type: "multiselect", Options: []string{"a", "b"}}
	if v, err := normalizeCustomFieldValue(multiDef, []interface{}{" a ", "b", "a"}); err != nil || v != "a,b" {
		t.Fatalf("multiselect normalize failed: %q %v", v, err)
	}
	if _, err := normalizeCustomFieldValue(multiDef, []interface{}{"c"}); err == nil {
		t.Fatal("expected invalid multiselect option error")
	}
	if _, err := normalizeCustomFieldValue(multiDef, 9); err == nil {
		t.Fatal("expected multiselect type error")
	}

	if _, err := normalizeCustomFieldValue(CustomFieldDefinition{Type: "weird"}, "x"); err == nil {
		t.Fatal("expected unsupported type error")
	}
}

func TestCoverageValidateStringFieldRegexBranches(t *testing.T) {
	if err := validateStringField(CustomFieldValidation{Regex: "[a-z]+"}, "abc"); err != nil {
		t.Fatalf("expected regex match, got %v", err)
	}
	if err := validateStringField(CustomFieldValidation{Regex: "^[0-9]+$"}, "abc"); err == nil {
		t.Fatal("expected regex mismatch error")
	}
	if err := validateStringField(CustomFieldValidation{Regex: "("}, "abc"); err == nil || err.Error() != "invalid regex" {
		t.Fatalf("expected invalid regex error, got %v", err)
	}
	if err := validateStringField(CustomFieldValidation{}, "abc"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCoverageValidateNumberAndOption(t *testing.T) {
	if err := validateNumberField(CustomFieldValidation{}, 5); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if err := validateNumberField(CustomFieldValidation{Min: f64Ptr(1)}, 1); err != nil {
		t.Fatalf("expected boundary ok, got %v", err)
	}
	if err := validateNumberField(CustomFieldValidation{Max: f64Ptr(1)}, 1); err != nil {
		t.Fatalf("expected boundary ok, got %v", err)
	}

	if err := validateOption(nil, "", true); err != nil {
		t.Fatalf("expected empty allowed, got %v", err)
	}
	if err := validateOption(nil, "", false); err == nil || err.Error() != "value required" {
		t.Fatalf("expected value required, got %v", err)
	}
	if err := validateOption(nil, "x", false); err != nil {
		t.Fatalf("expected no options to allow any, got %v", err)
	}
	if err := validateOption([]string{"x"}, " x ", false); err != nil {
		t.Fatalf("expected contained option, got %v", err)
	}
	if err := validateOption([]string{"x"}, "y", false); err == nil || err.Error() != "invalid option" {
		t.Fatalf("expected invalid option, got %v", err)
	}
}

func TestCoverageNormalizeNumber(t *testing.T) {
	cases := []struct {
		input interface{}
		want  float64
	}{
		{float64(1.5), 1.5},
		{float32(2.5), 2.5},
		{int(3), 3},
		{int64(4), 4},
		{int32(5), 5},
		{uint(6), 6},
		{uint64(7), 7},
		{" 8.5 ", 8.5},
	}
	for _, tc := range cases {
		got, err := normalizeNumber(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("normalizeNumber(%v): expected %v, got %v (%v)", tc.input, tc.want, got, err)
		}
	}
	if _, err := normalizeNumber("   "); err == nil || err.Error() != "empty number" {
		t.Fatalf("expected empty number error, got %v", err)
	}
	if _, err := normalizeNumber("abc"); err == nil || err.Error() != "invalid number" {
		t.Fatalf("expected invalid number error, got %v", err)
	}
	if _, err := normalizeNumber(struct{}{}); err == nil || err.Error() != "invalid number" {
		t.Fatalf("expected invalid number type error, got %v", err)
	}
}

func TestCoverageNormalizeBool(t *testing.T) {
	for _, input := range []interface{}{true, "TRUE", "1", "yes", "y"} {
		got, err := normalizeBool(input)
		if err != nil || !got {
			t.Fatalf("normalizeBool(%v): expected true, got %v (%v)", input, got, err)
		}
	}
	for _, input := range []interface{}{false, "FALSE", "0", "no", "n"} {
		got, err := normalizeBool(input)
		if err != nil || got {
			t.Fatalf("normalizeBool(%v): expected false, got %v (%v)", input, got, err)
		}
	}
	if _, err := normalizeBool("maybe"); err == nil {
		t.Fatal("expected invalid boolean string error")
	}
	if _, err := normalizeBool(3); err == nil {
		t.Fatal("expected invalid boolean type error")
	}
}

func TestCoverageNormalizeDate(t *testing.T) {
	if v, err := normalizeDate(""); err != nil || v != "" {
		t.Fatalf("empty date: %q %v", v, err)
	}
	if v, err := normalizeDate("2024-01-02"); err != nil || v != "2024-01-02" {
		t.Fatalf("short date: %q %v", v, err)
	}
	if v, err := normalizeDate("2024-01-02T08:30:00+02:00"); err != nil || v != "2024-01-02" {
		t.Fatalf("rfc3339 date: %q %v", v, err)
	}
	if _, err := normalizeDate("03-2024-01"); err == nil {
		t.Fatal("expected invalid date error")
	}
	if _, err := normalizeDate("20240102T080000Z"); err == nil {
		t.Fatal("expected invalid rfc3339 error")
	}
}

func TestCoverageNormalizeStringList(t *testing.T) {
	if got, _ := normalizeStringListAny([]string{" a ", "", "a", "b"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected []string normalize: %+v", got)
	}
	if got, _ := normalizeStringListAny([]interface{}{"a", 1}); len(got) != 2 || got[1] != "1" {
		t.Fatalf("unexpected []interface{} normalize: %+v", got)
	}
	if got, _ := normalizeStringListAny("a, b ,a"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected string split normalize: %+v", got)
	}
	if _, err := normalizeStringListAny(42); err == nil || err.Error() != "expected string list" {
		t.Fatalf("expected type error, got %v", err)
	}
	if got := normalizeStringList(42); got != nil {
		t.Fatalf("expected nil on error, got %+v", got)
	}
}

func TestCoverageIsEmptyCustomFieldValue(t *testing.T) {
	if !isEmptyCustomFieldValue(nil) {
		t.Fatal("nil should be empty")
	}
	if !isEmptyCustomFieldValue("   ") {
		t.Fatal("blank string should be empty")
	}
	if isEmptyCustomFieldValue("x") {
		t.Fatal("non-blank string should not be empty")
	}
	if !isEmptyCustomFieldValue([]string{" ", ""}) {
		t.Fatal("blank string slice should be empty")
	}
	if !isEmptyCustomFieldValue([]interface{}{" ", ""}) {
		t.Fatal("blank interface slice should be empty")
	}
	if !isEmptyCustomFieldValue([]interface{}{}) {
		t.Fatal("empty interface slice should be empty")
	}
	if isEmptyCustomFieldValue([]interface{}{1}) {
		t.Fatal("non-empty interface slice should not be empty")
	}
	if isEmptyCustomFieldValue(0) {
		t.Fatal("zero number formats to \"0\" and should not be empty")
	}
}

func TestCoverageBuildModelCustomFieldValues(t *testing.T) {
	fields := []models.CustomField{
		{ID: 1, Resource: "ticket", Key: "env", Name: "Env", Type: "select", Active: true, OptionsJSON: `["prod","dev"]`},
	}
	values, err := BuildModelCustomFieldValues(fields, map[string]interface{}{"env": "prod"}, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(values) != 1 || values[0].CustomFieldID != 1 || values[0].Value != "prod" || values[0].CreatedAt.IsZero() {
		t.Fatalf("unexpected values: %+v", values)
	}

	if values, err = BuildModelCustomFieldValues(fields, nil, nil, false); err != nil || values != nil {
		t.Fatalf("expected nil values for empty input, got %+v %v", values, err)
	}

	_, err = BuildModelCustomFieldValues(fields, map[string]interface{}{"env": "bogus"}, nil, false)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCoveragePrepareCustomFieldMutationBranches(t *testing.T) {
	fields := []models.CustomField{
		{ID: 1, Resource: "ticket", Key: "env", Name: "Env", Type: "select", Active: true, OptionsJSON: `["prod"]`},
		{ID: 2, Resource: "ticket", Key: "tier", Name: "Tier", Type: "string", Active: true, ShowWhenJSON: `{"all":[{"field":"ticket.priority","op":"eq","value":"high"}]}`},
	}

	mutation, err := PrepareCustomFieldMutation(fields, 1, nil, nil)
	if err != nil || mutation != nil {
		t.Fatalf("expected nil mutation for nil provided, got %+v %v", mutation, err)
	}

	mutation, err = PrepareCustomFieldMutation(fields, 1, map[string]interface{}{}, nil)
	if err != nil || mutation == nil || !mutation.ClearAll {
		t.Fatalf("expected clear-all mutation, got %+v %v", mutation, err)
	}

	mutation, err = PrepareCustomFieldMutation(fields, 1, map[string]interface{}{"env": "prod", "tier": "gold", "unknown": "x"}, nil)
	if err == nil || err.Error() != "unknown custom field: unknown" {
		t.Fatalf("expected unknown field error, got %v", err)
	}

	// delete empty value, skip hidden tier, upsert env
	mutation, err = PrepareCustomFieldMutation(fields, 1, map[string]interface{}{"env": "prod", "tier": ""}, map[string]interface{}{"ticket.priority": "low"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mutation.DeleteFieldIDs) != 1 || mutation.DeleteFieldIDs[0] != 2 {
		t.Fatalf("expected tier delete for empty value, got %+v", mutation.DeleteFieldIDs)
	}

	mutation, err = PrepareCustomFieldMutation(fields, 1, map[string]interface{}{"env": "prod", "tier": " "}, map[string]interface{}{"ticket.priority": "high"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mutation.Upserts) != 1 || mutation.Upserts[0].Key != "env" {
		t.Fatalf("expected env upsert only, got %+v", mutation.Upserts)
	}
	if len(mutation.DeleteFieldIDs) != 1 || mutation.DeleteFieldIDs[0] != 2 {
		t.Fatalf("expected tier delete, got %+v", mutation.DeleteFieldIDs)
	}

	mutation, err = PrepareCustomFieldMutation(fields, 1, map[string]interface{}{"env": " "}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mutation.DeleteFieldIDs) != 1 || mutation.DeleteFieldIDs[0] != 1 {
		t.Fatalf("expected delete for emptied field, got %+v", mutation.DeleteFieldIDs)
	}

	_, err = PrepareCustomFieldMutation(fields, 1, map[string]interface{}{"env": "bogus"}, nil)
	if err == nil {
		t.Fatal("expected normalize error")
	}
}

func TestCoverageMapMutationToModelValuesBranches(t *testing.T) {
	if got := MapMutationToModelValues(1, nil); got != nil {
		t.Fatalf("expected nil for nil mutation, got %+v", got)
	}
	if got := MapMutationToModelValues(1, &CustomFieldMutation{}); got != nil {
		t.Fatalf("expected nil for empty upserts, got %+v", got)
	}
	got := MapMutationToModelValues(7, &CustomFieldMutation{Upserts: []domain.CustomFieldValue{{CustomFieldID: 3, Value: "v"}}})
	if len(got) != 1 || got[0].TicketID != 7 || got[0].CustomFieldID != 3 || got[0].Value != "v" {
		t.Fatalf("unexpected model values: %+v", got)
	}
}

func TestCoverageParseCustomFieldHelpers(t *testing.T) {
	if got := parseCustomFieldOptions(""); got != nil {
		t.Fatalf("expected nil options, got %+v", got)
	}
	if got := parseCustomFieldOptions("{bad"); got != nil {
		t.Fatalf("expected nil on bad json, got %+v", got)
	}
	if got := parseCustomFieldOptions(`["a","b"]`); len(got) != 2 {
		t.Fatalf("expected parsed options, got %+v", got)
	}

	empty := parseCustomFieldValidation("")
	if empty.Min != nil || empty.Max != nil || empty.MinLength != nil || empty.MaxLength != nil || empty.Regex != "" {
		t.Fatalf("expected zero validation, got %+v", empty)
	}
	if got := parseCustomFieldValidation("{bad"); got.Min != nil {
		t.Fatalf("expected zero validation on bad json, got %+v", got)
	}
	got := parseCustomFieldValidation(`{"min":1,"max":2,"min_length":1,"max_length":5,"regex":"^a"}`)
	if *got.Min != 1 || *got.Max != 2 || *got.MinLength != 1 || *got.MaxLength != 5 || got.Regex != "^a" {
		t.Fatalf("expected parsed validation, got %+v", got)
	}

	if parseCustomFieldCondition("") != nil {
		t.Fatal("expected nil condition for empty json")
	}
	if parseCustomFieldCondition("{bad") != nil {
		t.Fatal("expected nil condition for bad json")
	}
	if parseCustomFieldCondition(`{"all":[]}`) != nil {
		t.Fatal("expected nil condition for empty all/any")
	}
	if parseCustomFieldCondition(`[]`) != nil {
		t.Fatal("expected nil condition for empty clause array")
	}
	if parseCustomFieldCondition(`[{"field":"a"}]`) == nil {
		t.Fatal("expected condition for clause array")
	}
	cond := parseCustomFieldCondition(`{"all":[{"field":"a","op":"eq","value":1}],"any":[{"field":"b"}]}`)
	if cond == nil || len(cond.All) != 1 || cond.All[0].Field != "a" || len(cond.Any) != 1 {
		t.Fatalf("expected parsed condition, got %+v", cond)
	}
	cond = parseCustomFieldCondition(`[{"field":"a","op":"ne","value":"x"}]`)
	if cond == nil || len(cond.All) != 1 || cond.All[0].Op != "ne" {
		t.Fatalf("expected clause-array condition, got %+v", cond)
	}

	defs := MapCustomFieldDefinitions([]models.CustomField{{ID: 1, Key: "k", Type: "string"}})
	if len(defs) != 1 || defs[0].Key != "k" {
		t.Fatalf("unexpected definitions: %+v", defs)
	}
}

func TestCoverageMapTicketDetailsNil(t *testing.T) {
	if got := MapTicketDetails(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestCoverageQueryServiceBranches(t *testing.T) {
	ctx := context.Background()

	if _, err := NewQueryService(stubQueryRepo{}).GetTicketByID(ctx, 0); err == nil || err.Error() != "ticket id required" {
		t.Fatalf("expected ticket id required, got %v", err)
	}
	if _, err := NewQueryService(stubQueryRepo{err: context.Canceled}).GetTicketByID(ctx, 1); err == nil {
		t.Fatal("expected repo error")
	}
	if _, err := NewQueryService(stubQueryRepo{err: context.Canceled}).ListTickets(ctx, ListTicketsQuery{Page: 1, PageSize: 1}); err == nil {
		t.Fatal("expected repo error")
	}
	result, err := NewQueryService(stubQueryRepo{items: []domain.Ticket{{ID: 2}}, total: 1}).ListTickets(ctx, ListTicketsQuery{})
	if err != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != 2 {
		t.Fatalf("unexpected result: %+v %v", result, err)
	}
}

func TestCoverageStatusPolicyValidate(t *testing.T) {
	policy := NewStatusTransitionPolicy()
	if err := policy.Validate("open", ""); err != nil {
		t.Fatalf("expected empty target allowed, got %v", err)
	}
	if err := policy.Validate("open", "open"); err != nil {
		t.Fatalf("expected same status allowed, got %v", err)
	}
	valid := [][2]string{
		{"", "assigned"}, {"open", "resolved"}, {"open", "closed"},
		{"assigned", "open"}, {"assigned", "in_progress"}, {"assigned", "resolved"}, {"assigned", "closed"},
		{"in_progress", "open"}, {"in_progress", "resolved"}, {"in_progress", "closed"},
		{"resolved", "closed"}, {"closed", "closed"},
	}
	for _, pair := range valid {
		if err := policy.Validate(pair[0], pair[1]); err != nil {
			t.Fatalf("expected %v -> %v allowed, got %v", pair[0], pair[1], err)
		}
	}
	if err := policy.Validate("assigned", "bogus"); err == nil {
		t.Fatal("expected invalid transition error")
	}
	if err := policy.Validate("resolved", "open"); err == nil {
		t.Fatal("expected invalid transition error")
	}
	if err := policy.Validate("closed", "open"); err == nil {
		t.Fatal("expected invalid transition error")
	}
	if err := policy.Validate("bogus", "open"); err == nil {
		t.Fatal("expected invalid transition error")
	}
}

var _ = intPtrCf
