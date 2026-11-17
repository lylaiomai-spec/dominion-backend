package test

import (
	"cuento-backend/src/Controllers"
	"testing"
)

func TestMybbUserKey_DistinguishesUsers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		idA, nameA string
		idB, nameB string
		expectSame bool
	}{
		{"deleted users share id 1", "1", "papa gede", "1", "baron samedi", false},
		{"same user twice", "42", "papa gede", "42", "papa gede", true},
		{"same name, different id", "1", "papa gede", "42", "papa gede", false},
		{"colon in name stays distinct", "1", "papa:gede", "1", "papa", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Controllers.MybbUserKey(tt.idA, tt.nameA)
			b := Controllers.MybbUserKey(tt.idB, tt.nameB)
			if (a == b) != tt.expectSame {
				t.Errorf("keys %q and %q: expected same=%v", a, b, tt.expectSame)
			}
		})
	}
}

func TestDecodeUserCharacterMap_Keys(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	tests := []struct {
		name     string
		raw      *string
		expected []string
	}{
		{"nil map", nil, []string{}},
		{"empty map", ptr(`{}`), []string{}},
		{
			"legacy id keys are rekeyed",
			ptr(`{"5":{"original_user_id":"5","original_user_name":"loa"}}`),
			[]string{"5:loa"},
		},
		{
			"composite keys kept",
			ptr(`{"5:loa":{"key":"5:loa","original_user_id":"5","original_user_name":"loa"}}`),
			[]string{"5:loa"},
		},
		{
			"deleted users stay separate",
			ptr(`{"1:papa gede":{"original_user_id":"1","original_user_name":"papa gede"},"1:baron samedi":{"original_user_id":"1","original_user_name":"baron samedi"}}`),
			[]string{"1:papa gede", "1:baron samedi"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Controllers.DecodeUserCharacterMap(tt.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d entries, got %d: %v", len(tt.expected), len(got), got)
			}
			for _, key := range tt.expected {
				entry, ok := got[key]
				if !ok {
					t.Errorf("missing key %q in %v", key, got)
					continue
				}
				if entry.Key != key {
					t.Errorf("entry under %q has Key %q", key, entry.Key)
				}
			}
		})
	}
}

func TestDecodeUserCharacterMap_InvalidJSON(t *testing.T) {
	t.Parallel()

	raw := `{not json`
	if _, err := Controllers.DecodeUserCharacterMap(&raw); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}

func TestDecodeUserCharacterMap_KeepsMapping(t *testing.T) {
	t.Parallel()

	raw := `{"7":{"original_user_id":"7","original_user_name":"loa","character_id":3,"character_name":"Erzulie","user_id":2}}`
	got, err := Controllers.DecodeUserCharacterMap(&raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := got["7:loa"]
	if entry.CharacterId == nil || *entry.CharacterId != 3 {
		t.Errorf("expected character_id 3, got %v", entry.CharacterId)
	}
	if entry.CharacterName == nil || *entry.CharacterName != "Erzulie" {
		t.Errorf("expected character_name Erzulie, got %v", entry.CharacterName)
	}
}
