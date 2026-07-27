package pingcodert

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestConvertAPIDocBuildsStableOperationsAndMergesSameRoute(t *testing.T) {
	source := []byte(`[
  {
    "type":"GET",
    "url":"/v1/pjm/work_items/{work_item_id}?include={include}",
    "group":"工作项",
    "name":"获取工作项",
    "permission":[{"name":"企业令牌/用户令牌"}],
    "scopes":[{"name":"pcp:read:pjm:workitem"}],
    "parameter":{"fields":{"路径参数":[{"type":"String","optional":false,"field":"work_item_id"}],"查询参数":[{"type":"Boolean","optional":false,"field":"include"}]}},
    "success":{"examples":[{"type":"json","content":"{\"id\":\"WI-1\"}"}]}
  },
  {
    "type":"POST",
    "url":"/v1/attachments",
    "group":"附件",
    "name":"上传代码段",
    "permission":[{"name":"企业令牌"}],
    "parameter":{"fields":{"Parameter":[{"type":"String","optional":false,"field":"content"}]}}
  },
  {
    "type":"POST",
    "url":"/v1/attachments?principal_type={principal_type}",
    "group":"附件",
    "name":"上传文件",
    "permission":[{"name":"企业令牌"}],
    "parameter":{"fields":{"查询参数":[{"type":"String","optional":false,"field":"principal_type"}],"请求参数 form-data":[{"type":"File","optional":false,"field":"file"}]}}
  },
  {"type":"get","url":"/v1/auth/token?grant_type=client_credentials","name":"获取企业令牌"}
]`)

	converted, err := ConvertAPIDoc(source)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(converted, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	if len(paths) != 2 {
		t.Fatalf("paths = %d, want 2: %s", len(paths), converted)
	}
	get := paths["/v1/pjm/work_items/{work_item_id}"].(map[string]any)["get"].(map[string]any)
	if got := get["operationId"]; got != "get-work-items-by-work-item-id-by-include" {
		t.Fatalf("operationId = %v", got)
	}
	security := get["security"].([]any)
	if len(security) != 2 {
		t.Fatalf("security alternatives = %d, want 2", len(security))
	}
	post := paths["/v1/attachments"].(map[string]any)["post"].(map[string]any)
	content := post["requestBody"].(map[string]any)["content"].(map[string]any)
	if content["application/json"] == nil || content["multipart/form-data"] == nil {
		t.Fatalf("merged request content = %#v", content)
	}
	if bytes.Contains(converted, []byte("/v1/auth/token")) {
		t.Fatal("auth operation should be excluded")
	}
}

func TestConvertAPIDocRejectsUnknownFieldType(t *testing.T) {
	_, err := ConvertAPIDoc([]byte(`[{
      "type":"POST","url":"/v1/pjm/work_items","name":"create",
      "parameter":{"fields":{"Parameter":[{"type":"Mystery","field":"value"}]}}
    }]`))
	if err == nil || !strings.Contains(err.Error(), `unsupported type "Mystery"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestConvertAPIDocRejectsWriteWithoutRecognizedPermission(t *testing.T) {
	_, err := ConvertAPIDoc([]byte(`[{
	      "type":"POST","url":"/v1/pjm/work_items","name":"create",
	      "parameter":{"fields":{"Parameter":[{"type":"String","field":"title"}]}}
	    }]`))
	if err == nil || !strings.Contains(err.Error(), "write operation has no recognized PingCode token permission") {
		t.Fatalf("error = %v", err)
	}
}

func TestAPIDocLoaderDetectionIsSpecific(t *testing.T) {
	loader := APIDocLoader{}
	if loader.Detect("application/json", []byte(`[{"name":"not an API"}]`)) {
		t.Fatal("unrelated JSON array detected as PingCode API data")
	}
	if !loader.Detect("application/json", []byte(`[{"type":"GET","url":"/v1/pjm/projects","name":"projects"}]`)) {
		t.Fatal("PingCode API data was not detected")
	}
}

func TestOfficialAPIDocFile(t *testing.T) {
	path := os.Getenv("PINGCODE_API_DATA_FILE")
	if path == "" {
		t.Skip("set PINGCODE_API_DATA_FILE to validate a downloaded official api_data.json")
	}
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	converted, err := ConvertAPIDoc(source)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(converted, &document); err != nil {
		t.Fatal(err)
	}
	operations := 0
	for _, path := range document.Paths {
		operations += len(path)
	}
	if operations < 450 {
		t.Fatalf("generated operations = %d, want at least 450", operations)
	}
}
