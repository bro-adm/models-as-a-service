package configreconcile

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"

	_ "embed"
)

//go:embed templates/envoyfilter-usage-logs.yaml
var envoyFilterUsageLogsTemplate string

type envoyFilterTemplateData struct {
	Name      string
	Namespace string
	OTELHost  string
	OTELPort  int64
}

func parseOTELEndpoint(endpoint string) (host string, port int64, err error) {
	parts := strings.Split(endpoint, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid format %q: expected host:port", endpoint)
	}
	host = parts[0]
	port, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port in %q: %w", endpoint, err)
	}
	return host, port, nil
}

func renderEnvoyFilterTemplate(host string, port int64) (*unstructured.Unstructured, error) {
	data := envoyFilterTemplateData{
		Name:      EnvoyFilterUsageName,
		Namespace: EnvoyFilterNamespace,
		OTELHost:  host,
		OTELPort:  port,
	}

	tmpl, err := template.New("envoyfilter-usage").Parse(envoyFilterUsageLogsTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	ef := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(buf.Bytes(), &ef.Object); err != nil {
		return nil, fmt.Errorf("unmarshal YAML: %w", err)
	}

	return ef, nil
}
