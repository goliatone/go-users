package schema

import (
	"context"
	"net/http"
	"testing"

	"github.com/goliatone/go-router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRegistryDocumentCompilesProviders(t *testing.T) {
	reg := NewRegistry(WithInfo(router.OpenAPIInfo{
		Title:       "Test Schemas",
		Version:     "v1",
		Description: "Integration snapshot",
	}))

	reg.Register(newStubProvider("user"))
	reg.Register(newStubProvider("role"))

	doc := reg.Document()
	require.NotNil(t, doc)
	assert.Equal(t, "Test Schemas", doc["info"].(map[string]any)["title"])

	paths, ok := doc["paths"].(map[string]any)
	require.True(t, ok)
	_, ok = paths["/roles"]
	assert.True(t, ok, "expected /roles path to be present")
}

func TestRegistryHandlerEmitsNoContentWhenEmpty(t *testing.T) {
	reg := NewRegistry()
	ctx := router.NewMockContext()
	ctx.On("NoContent", http.StatusNoContent).Return(nil)

	require.NoError(t, reg.Handler()(ctx))
	ctx.AssertCalled(t, "NoContent", http.StatusNoContent)
}

func TestRegistryHandlerReturnsJSONPayload(t *testing.T) {
	reg := NewRegistry()
	reg.Register(newStubProvider("preference"))

	ctx := router.NewMockContext()
	ctx.On("JSON", http.StatusOK, mock.Anything).Return(nil)

	require.NoError(t, reg.Handler()(ctx))
	ctx.AssertCalled(t, "JSON", http.StatusOK, mock.Anything)
}

func TestRegistryListenerReceivesSnapshot(t *testing.T) {
	reg := NewRegistry()
	called := false
	reg.Subscribe(func(_ context.Context, snap Snapshot) {
		called = true
		require.Equal(t, []string{"user"}, snap.ResourceNames)
		require.NotNil(t, snap.Document)
	})

	reg.Register(newStubProvider("user"))
	assert.True(t, called, "expected listener to be invoked")
}

type stubProvider struct {
	metadata router.ResourceMetadata
}

func (s stubProvider) GetMetadata() router.ResourceMetadata {
	return s.metadata
}

func newStubProvider(name string) router.MetadataProvider {
	plural := name + "s"
	return stubProvider{
		metadata: router.ResourceMetadata{
			Name:       name,
			PluralName: plural,
			Schema: router.SchemaMetadata{
				Name: name,
				Properties: map[string]router.PropertyInfo{
					"id": {
						Type:         "string",
						OriginalName: "id",
					},
				},
			},
			Routes: []router.RouteDefinition{
				{
					Method: router.GET,
					Path:   "/" + plural,
					Name:   name + ":list",
				},
			},
		},
	}
}
