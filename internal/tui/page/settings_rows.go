package page

import (
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/tui/components/settings"
)

func headerField(key, label string) settings.Field {
	return settings.Field{
		Key:   key,
		Label: label,
		Type:  settings.FieldHeader,
	}
}

func noteField(key, label, value string, level settings.NoteLevel) settings.Field {
	return settings.Field{
		Key:       key,
		Label:     label,
		Value:     value,
		Type:      settings.FieldNote,
		NoteLevel: level,
	}
}

func infoNote(key, label, value string) settings.Field {
	return noteField(key, label, value, settings.NoteLevelInfo)
}

func warningNote(key, label, value string) settings.Field {
	return noteField(key, label, value, settings.NoteLevelWarning)
}

func errorNote(key, label, value string) settings.Field {
	return noteField(key, label, value, settings.NoteLevelError)
}

func nonEmptyTitle(primary, fallback string) string {
	if title := strings.TrimSpace(primary); title != "" {
		return title
	}
	return strings.TrimSpace(fallback)
}

func withCard(field settings.Field, card, status string) settings.Field {
	return withCardID(field, card, "", status)
}

func withCardID(field settings.Field, card, cardID, status string) settings.Field {
	field.Card = card
	field.CardID = cardID
	if status != "" {
		field.CardStatus = status
	}
	return field
}

func fieldDisplayLabel(field settings.Field) string {
	label := strings.TrimSpace(field.Label)
	card := strings.TrimSpace(field.Card)
	if card == "" || label == "" {
		return label
	}
	return card + " › " + label
}

func invalidFieldValueError(field settings.Field, err error) error {
	return fmt.Errorf("invalid value for %s: %w", fieldDisplayLabel(field), err)
}
