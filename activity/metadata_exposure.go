package activity

import (
	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/types"
)

// MetadataExposureStrategy controls how activity metadata is exposed on read.
type MetadataExposureStrategy int

const (
	// MetadataExposeNone returns no metadata.
	MetadataExposeNone MetadataExposureStrategy = iota
	// MetadataExposeSanitized returns metadata after sanitization.
	MetadataExposeSanitized
	// MetadataExposeAll returns raw metadata (intended for development/debug).
	MetadataExposeAll
)

// MetadataSanitizer customizes sanitized metadata exposure.
type MetadataSanitizer func(actor *auth.ActorContext, role string, record types.ActivityRecord) map[string]any
