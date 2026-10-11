package application

import (
	"encoding/json"
	"fmt"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/modules/ticket/domain"
)

type rawCustomFieldCondition struct {
	All []rawCustomFieldClause `json:"all"`
	Any []rawCustomFieldClause `json:"any"`
}

type rawCustomFieldClause struct {
	Field string      `json:"field"`
	Op    string      `json:"op"`
	Value interface{} `json:"value"`
}

type rawCustomFieldValidation struct {
	Min       *float64 `json:"min"`
	Max       *float64 `json:"max"`
	MinLength *int     `json:"min_length"`
	MaxLength *int     `json:"max_length"`
	Regex     string   `json:"regex"`
}

func MapCustomFieldDefinitions(fields []models.CustomField) []CustomFieldDefinition {
	out := make([]CustomFieldDefinition, 0, len(fields))
	for _, field := range fields {
		out = append(out, CustomFieldDefinition{
			ID:         field.ID,
			Key:        field.Key,
			Type:       field.Type,
			Required:   field.Required,
			Active:     field.Active,
			Options:    parseCustomFieldOptions(field.OptionsJSON),
			Validation: parseCustomFieldValidation(field.ValidationJSON),
			ShowWhen:   parseCustomFieldCondition(field.ShowWhenJSON),
		})
	}
	return out
}

func BuildModelCustomFieldValues(
	fields []models.CustomField,
	provided map[string]interface{},
	ticketContext map[string]interface{},
	enforceRequired bool,
) ([]models.TicketCustomFieldValue, error) {
	validator := NewCustomFieldValidator()
	values, err := validator.Validate(MapCustomFieldDefinitions(fields), provided, ticketContext, enforceRequired)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}

	now := time.Now()
	out := make([]models.TicketCustomFieldValue, 0, len(values))
	for _, value := range values {
		out = append(out, models.TicketCustomFieldValue{
			CustomFieldID: value.CustomFieldID,
			Value:         value.Value,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	return out, nil
}

func PrepareCustomFieldMutation(
	fields []models.CustomField,
	_ uint,
	provided map[string]interface{},
	ticketContext map[string]interface{},
) (*CustomFieldMutation, error) {
	if provided == nil {
		return nil, nil
	}

	definitions := MapCustomFieldDefinitions(fields)
	fieldByKey := make(map[string]CustomFieldDefinition, len(definitions))
	ctxMap := make(map[string]interface{}, len(ticketContext)+len(provided)+8)
	for k, value := range ticketContext {
		ctxMap[k] = value
	}
	for _, definition := range definitions {
		fieldByKey[definition.Key] = definition
	}
	for k, value := range provided {
		ctxMap[k] = value
		ctxMap["cf."+k] = value
	}

	mutation := &CustomFieldMutation{}
	if len(provided) == 0 {
		mutation.ClearAll = true
		return mutation, nil
	}

	for key, raw := range provided {
		definition, ok := fieldByKey[key]
		if !ok {
			return nil, fmt.Errorf("unknown custom field: %s", key)
		}
		if raw == nil || isEmptyCustomFieldValue(raw) {
			mutation.DeleteFieldIDs = append(mutation.DeleteFieldIDs, definition.ID)
			continue
		}
		if !definition.Active || !customFieldConditionMet(definition.ShowWhen, ctxMap) {
			continue
		}

		value, err := normalizeCustomFieldValue(definition, raw)
		if err != nil {
			return nil, fmt.Errorf("custom field %q: %w", key, err)
		}
		mutation.Upserts = append(mutation.Upserts, domain.CustomFieldValue{
			CustomFieldID: definition.ID,
			Key:           definition.Key,
			Value:         value,
		})
	}

	return mutation, nil
}

func MapMutationToModelValues(ticketID uint, mutation *CustomFieldMutation) []models.TicketCustomFieldValue {
	if mutation == nil || len(mutation.Upserts) == 0 {
		return nil
	}
	now := time.Now()
	out := make([]models.TicketCustomFieldValue, 0, len(mutation.Upserts))
	for _, value := range mutation.Upserts {
		out = append(out, models.TicketCustomFieldValue{
			TicketID:      ticketID,
			CustomFieldID: value.CustomFieldID,
			Value:         value.Value,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	return out
}

func parseCustomFieldOptions(optionsJSON string) []string {
	if optionsJSON == "" {
		return nil
	}
	var options []string
	if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
		return nil
	}
	return options
}

func parseCustomFieldValidation(validationJSON string) CustomFieldValidation {
	if validationJSON == "" {
		return CustomFieldValidation{}
	}
	var raw rawCustomFieldValidation
	if err := json.Unmarshal([]byte(validationJSON), &raw); err != nil {
		return CustomFieldValidation{}
	}
	return CustomFieldValidation(raw)
}

func parseCustomFieldCondition(showWhenJSON string) *CustomFieldCondition {
	if showWhenJSON == "" {
		return nil
	}
	var expr rawCustomFieldCondition
	if err := json.Unmarshal([]byte(showWhenJSON), &expr); err == nil && (len(expr.All) > 0 || len(expr.Any) > 0) {
		return &CustomFieldCondition{
			All: mapCustomFieldClauses(expr.All),
			Any: mapCustomFieldClauses(expr.Any),
		}
	}
	var clauses []rawCustomFieldClause
	if err := json.Unmarshal([]byte(showWhenJSON), &clauses); err == nil && len(clauses) > 0 {
		return &CustomFieldCondition{All: mapCustomFieldClauses(clauses)}
	}
	return nil
}

func mapCustomFieldClauses(clauses []rawCustomFieldClause) []CustomFieldClause {
	out := make([]CustomFieldClause, 0, len(clauses))
	for _, clause := range clauses {
		out = append(out, CustomFieldClause(clause))
	}
	return out
}
