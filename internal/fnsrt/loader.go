package fnsrt

import (
	"fmt"

	"github.com/0xfe10/aicli/internal/swagger2rt"
	"github.com/pb33f/libopenapi"
	restish "github.com/rest-sh/restish/v2"
)

const loaderPriority = 100

// SpecLoader converts FNS Swagger 2 documents to a filtered OpenAPI 3 surface.
type SpecLoader struct{}

func (SpecLoader) Priority() int { return loaderPriority }

func (SpecLoader) Detect(contentType string, body []byte) bool {
	_ = contentType
	return swagger2rt.IsSwagger2(body)
}

func (SpecLoader) LoadWithOptions(body []byte, _ restish.LoadOptions) (*restish.APISpec, error) {
	converted, err := ConvertAndFix(body)
	if err != nil {
		return nil, err
	}
	document, err := libopenapi.NewDocument(converted)
	if err != nil {
		return nil, fmt.Errorf("parse fixed FNS OpenAPI: %w", err)
	}
	return &restish.APISpec{
		ContentType: "application/json",
		Raw:         body,
		Document:    document,
	}, nil
}

// ConvertAndFix converts Swagger 2 and applies FNS contract fixes.
func ConvertAndFix(body []byte) ([]byte, error) {
	converted, err := swagger2rt.Convert(body)
	if err != nil {
		return nil, err
	}
	return FixSpec(converted)
}
