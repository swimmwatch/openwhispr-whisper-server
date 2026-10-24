package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
)

func chartPath() string {
	if value := os.Getenv("P06_CHART_DIR"); value != "" {
		return value
	}

	return "charts/whisper-server"
}

func chartInputHash(t *testing.T, kind, version, field string, data []byte) {
	t.Helper()

	var inputs []map[string]string
	if err := json.Unmarshal(artifact(t, "tests/integration/testdata/chart-inputs.json"), &inputs); err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		if input["kind"] == kind && input["version"] == version {
			if fmt.Sprintf("%x", sha256.Sum256(data)) != input[field] {
				t.Fatalf("%s %s identity mismatch", kind, version)
			}
			return
		}
	}
	t.Fatalf("missing pinned %s %s input", kind, version)
}

func chartTools(t *testing.T) []string {
	t.Helper()

	if os.Getenv("P06_CHART_TESTS") != "1" {
		t.Skip("chart matrix requires P06_CHART_TESTS=1 and the pinned local harness")
	}

	isolation := t.TempDir()
	for key, relative := range map[string]string{
		"HELM_CACHE_HOME":        "cache",
		"HELM_CONFIG_HOME":       "config",
		"HELM_DATA_HOME":         "data",
		"HELM_REPOSITORY_CONFIG": "repositories.yaml",
		"HELM_REPOSITORY_CACHE":  "repository",
		"HELM_REGISTRY_CONFIG":   "registry.json",
		"HELM_PLUGINS":           "plugins",
	} {
		t.Setenv(key, filepath.Join(isolation, relative))
	}
	t.Setenv("KUBECONFIG", "/dev/null")
	var result []string
	for _, major := range []string{"3", "4"} {
		binary := os.Getenv("P06_HELM" + major)
		if binary == "" {
			binary = filepath.Join(root(t), ".local/p06/bin/helm"+major)
		}

		version := string(run(t, nil, binary, "version", "--short"))
		if !strings.HasPrefix(version, map[string]string{
			"3": "v3.22.0+",
			"4": "v4.3.0+",
		}[major]) {
			t.Fatalf("unexpected Helm matrix executable: %s", version)
		}

		data, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		chartInputHash(t, "helm", strings.TrimPrefix(strings.Split(version, "+")[0], "v"), "binary_sha256", data)
		result = append(result, binary)
	}

	return result
}

func chartArgs(t *testing.T, kube string, values map[string]any) []string {
	t.Helper()

	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(t.TempDir(), "values.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}

	return []string{
		"template", "whisper", chartPath(), "--namespace", "whisper-test",
		"--kube-version", kube, "--values", file,
	}
}

func chartRender(t *testing.T, helm, kube string, values map[string]any) []map[string]any {
	t.Helper()

	manifest := run(t, nil, helm, chartArgs(t, kube, values)...)
	saved := filepath.Join(t.TempDir(), "rendered.yaml")
	if err := os.WriteFile(saved, manifest, 0600); err != nil {
		t.Fatal(err)
	}

	python := pythonBinary()
	data := run(
		t, nil, python, "-c",
		"import json,sys,yaml; print(json.dumps([x for x in yaml.safe_load_all(open(sys.argv[1])) if x]))",
		saved)
	var result []map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	return result
}

func chartObject(t *testing.T, resources []map[string]any, kind, suffix string) map[string]any {
	t.Helper()

	for _, resource := range resources {
		if resource["kind"] == kind && strings.HasSuffix(object(t, resource["metadata"])["name"].(string), suffix) {
			return resource
		}
	}
	t.Fatalf("missing %s ending %s", kind, suffix)
	return nil
}

func chartConfig(t *testing.T, resources []map[string]any) (config.Settings, string) {
	t.Helper()

	data := object(t, chartObject(t, resources, "ConfigMap", "-config")["data"])["config.json"].(string)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	settings, err := config.Load([]string{"--config", path}, []string{})
	if err != nil {
		t.Fatalf("rendered configuration rejected by the actual Go loader: %v", err)
	}

	return settings, data
}

func TestChartMatrix(t *testing.T) {
	for _, helm := range chartTools(t) {
		run(t, nil, helm, "lint", chartPath(), "--strict")
		for _, kube := range []string{"1.35.0", "1.36.0", "1.37.0"} {
			for _, values := range []map[string]any{
				{},
				{
					"replicaCount": 2,
					"images": map[string]any{
						"api":     map[string]any{"repository": "docker.io/swimmwatch/openwhispr-whisper-server-api"},
						"backend": map[string]any{"repository": "docker.io/swimmwatch/openwhispr-whisper-server-backend"},
					},
				},
				{
					"replicaCount": 2,
					"cache": map[string]any{
						"existingClaim": "operator-cache",
						"accessModes":   []string{"ReadWriteMany"},
					},
					"ingress": map[string]any{
						"enabled":       true,
						"host":          "whisper.example",
						"className":     "nginx",
						"tlsSecretName": "operator-tls",
					},
				},
				{
					"prepared": map[string]any{"existingClaim": "operator-models"},
					"config": map[string]any{
						"models": map[string]any{
							"catalog": []any{
								map[string]any{
									"id": "custom",
									"source": map[string]any{
										"kind": "prepared",
										"path": "/prepared/base.bin",
									},
								},
							},
							"served":      "custom",
							"downloadSet": []string{},
						},
					},
				},
				{
					"images": map[string]any{
						"api":     map[string]any{"digest": "sha256:" + strings.Repeat("a", 64)},
						"backend": map[string]any{"digest": "sha256:" + strings.Repeat("b", 64)},
					},
					"api": map[string]any{
						"nodeSelector": map[string]string{"node-role": "api"},
						"tolerations": []any{
							map[string]any{
								"key":      "dedicated",
								"operator": "Equal",
								"value":    "whisper",
								"effect":   "NoSchedule",
							},
						},
						"affinity": map[string]any{
							"nodeAffinity": map[string]any{
								"requiredDuringSchedulingIgnoredDuringExecution": map[string]any{
									"nodeSelectorTerms": []any{
										map[string]any{
											"matchExpressions": []any{
												map[string]any{
													"key":      "kubernetes.io/os",
													"operator": "In",
													"values":   []string{"linux"},
												},
											},
										},
									},
								},
							},
						},
					},
					"backend": map[string]any{"runtimeClassName": "nvidia"},
				},
			} {
				resources := chartRender(t, helm, kube, values)
				settings, _ := chartConfig(t, resources)
				if settings.Mode != "resident" {
					t.Fatal("chart changed resident mode")
				}
				if overrides, ok := values["config"].(map[string]any); ok {
					if models, ok := overrides["models"].(map[string]any); ok {
						if settings.Models.Served != models["served"] ||
							len(settings.Models.Catalog) != 1 ||
							settings.Models.Catalog[0].ID != "custom" ||
							len(settings.Models.DownloadSet) != 0 {
							t.Fatal("model override retained bootstrap configuration")
						}
					}
				}
				checkChartStorageAndProbes(t, resources, settings, values)
				python := pythonBinary()
				schemaDir := os.Getenv("P06_SCHEMA_DIR")
				if schemaDir == "" {
					schemaDir = filepath.Join(root(t), ".local/p06/schemas")
				}

				schemaBytes, err := os.ReadFile(filepath.Join(schemaDir, kube+".json"))
				if err != nil {
					t.Fatal(err)
				}
				chartInputHash(t, "kubernetes", kube, "sha256", schemaBytes)
				data, _ := json.Marshal(resources)
				run(t, data, python, "-c", `import json,sys,jsonschema
schema=json.load(open(sys.argv[1]))
def normalize(value):
 if isinstance(value,dict):
  if value.get('format')=='int-or-string': value['type']=['integer','string']
  if value.get('type')=='integer' and value.get('format') in ('int32','int64'):
   bits=32 if value['format']=='int32' else 64
   value.setdefault('minimum',-(2**(bits-1)))
   value.setdefault('maximum',2**(bits-1)-1)
  if value.get('type')=='object' and 'properties' in value: value.setdefault('additionalProperties',False)
  for nested in value.values(): normalize(nested)
 elif isinstance(value,list):
  for nested in value: normalize(nested)
normalize(schema)
resolver=jsonschema.RefResolver.from_schema(schema)
for resource in json.load(sys.stdin):
 group,version=(resource['apiVersion'].split('/') if '/' in resource['apiVersion'] else ('',resource['apiVersion']))
 matches=[value for value in schema['definitions'].values() if {'group':group,'version':version,'kind':resource['kind']} in value.get('x-kubernetes-group-version-kind',[])]
 assert len(matches)==1, (resource['apiVersion'],resource['kind'])
 jsonschema.Draft4Validator(matches[0],resolver=resolver).validate(resource)
print('PASS: official Kubernetes API schemas')`, filepath.Join(schemaDir, kube+".json"))
			}
		}

		archive := t.TempDir()
		run(t, nil, helm, "package", chartPath(), "--destination", archive)
		files, err := filepath.Glob(filepath.Join(archive, "*.tgz"))
		if err != nil || len(files) != 1 {
			t.Fatal("missing packaged chart")
		}
		verifyChartArchive(t, files[0])
		run(t, nil, helm, "show", "chart", files[0])
		run(t, nil, helm, "template", "whisper", files[0], "--namespace", "whisper-test")
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		server := &http.Server{
			Handler:           http.FileServer(http.Dir(archive)),
			ReadHeaderTimeout: time.Second,
			WriteTimeout:      3 * time.Second,
		}

		done := make(chan error, 1)
		go func() {
			done <- server.Serve(listener)
		}()
		t.Cleanup(func() {
			_ = server.Close()
			<-done
		})

		url := "http://" + listener.Addr().String()
		run(t, nil, helm, "repo", "index", archive, "--url", url)
		pulled := t.TempDir()
		version := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(files[0]), ".tgz"), "whisper-server-")
		run(t, nil, helm, "pull", "whisper-server", "--version", version, "--repo", url, "--destination", pulled)
		original, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}

		copy, err := os.ReadFile(filepath.Join(pulled, filepath.Base(files[0])))
		if err != nil || !bytes.Equal(original, copy) {
			t.Fatal("local Helm pull changed archive bytes")
		}
	}
}

func TestChartMetadata(t *testing.T) {
	metadata := yamlObject(t, filepath.Join(chartPath(), "Chart.yaml"))
	annotations := object(t, metadata["annotations"])
	if annotations["org.opencontainers.image.licenses"] != "Apache-2.0" || annotations["artifacthub.io/license"] != "Apache-2.0" {
		t.Fatal("chart license metadata must describe project-owned chart content")
	}
	for _, field := range []string{"home", "description", "sources", "maintainers", "keywords"} {
		if metadata[field] == nil {
			t.Fatalf("missing chart metadata %s", field)
		}
	}
	for _, helm := range chartTools(t) {
		t.Run(filepath.Base(helm), func(t *testing.T) {
			directory := t.TempDir()
			run(t, nil, helm, "package", chartPath(), "--destination", directory)
			archives, err := filepath.Glob(filepath.Join(directory, "*.tgz"))
			if err != nil || len(archives) != 1 {
				t.Fatal("missing packaged chart")
			}

			observed := run(t, nil, helm, "show", "chart", archives[0])
			path := filepath.Join(t.TempDir(), "metadata.yaml")
			if err := os.WriteFile(path, observed, 0600); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(yamlObject(t, path), metadata) {
				t.Fatal("packaging changed chart metadata")
			}
			run(t, nil, helm, "repo", "index", directory, "--url", "https://example.invalid/charts")
			index := yamlObject(t, filepath.Join(directory, "index.yaml"))
			entries := object(t, index["entries"])["whisper-server"].([]any)
			if len(entries) != 1 {
				t.Fatal("expected one chart repository entry")
			}

			entry := object(t, entries[0])
			for field, expected := range metadata {
				if !reflect.DeepEqual(entry[field], expected) {
					t.Fatalf("repository index changed chart metadata %s", field)
				}
			}
		})
	}
}

func verifyChartArchive(t *testing.T, path string) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()

	reader := tar.NewReader(compressed)
	seen := map[string]bool{}
	for {
		entry, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}

		name := strings.TrimPrefix(entry.Name, "whisper-server/")
		if entry.Typeflag != tar.TypeReg ||
			!(strings.HasPrefix(name, "templates/") ||
				slices.Contains([]string{
					".helmignore", "Chart.yaml", "values.yaml", "values.schema.json",
					"LICENSE", "NOTICE", "files/defaults.json", "README.md",
				}, name)) {
			t.Fatalf("unexpected packaged path: %s", name)
		}
		seen[name] = true
		if name == "LICENSE" || name == "files/defaults.json" {
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}

			source := "LICENSE"
			if name != source {
				source = "internal/platform/config/defaults.json"
			}
			if !bytes.Equal(data, artifact(t, source)) {
				t.Fatal("package license/defaults drift")
			}
		}
	}
	for _, name := range []string{
		"Chart.yaml", "values.yaml", "values.schema.json", "LICENSE", "NOTICE", "files/defaults.json",
		"templates/statefulsets.yaml", "templates/networkpolicy.yaml",
	} {
		if !seen[name] {
			t.Fatalf("missing packaged path %s", name)
		}
	}
}

func checkChartStorageAndProbes(t *testing.T, resources []map[string]any, settings config.Settings, values map[string]any) {
	t.Helper()

	for _, role := range []string{"api", "backend"} {
		sts := chartObject(t, resources, "StatefulSet", "-"+role)
		spec := object(t, sts["spec"])
		pod := object(t, object(t, spec["template"])["spec"])
		container := object(t, pod["containers"].([]any)[0])
		security := object(t, pod["securityContext"])
		if security["runAsNonRoot"] != true || security["runAsUser"] != float64(10001) {
			t.Fatal("non-root Pod identity changed")
		}

		startup := object(t, container["startupProbe"])
		if startup["failureThreshold"].(float64)*startup["periodSeconds"].(float64) <
			float64(settings.Timeouts.ProvisioningSeconds+
				settings.Timeouts.OperationSeconds+settings.Timeouts.CleanupSeconds) {
			t.Fatal("short startup budget")
		}

		mounts := map[string]map[string]any{}
		for _, raw := range container["volumeMounts"].([]any) {
			mount := object(t, raw)
			mounts[mount["name"].(string)] = mount
		}
		if mounts["config"]["readOnly"] != true || mounts["transient"]["mountPath"] != "/tmp" {
			t.Fatal("configuration/scratch mount boundary changed")
		}

		volumes := map[string]map[string]any{}
		for _, raw := range pod["volumes"].([]any) {
			volume := object(t, raw)
			volumes[volume["name"].(string)] = volume
		}

		scratch := object(t, volumes["transient"]["emptyDir"])
		if scratch["sizeLimit"] == nil || scratch["medium"] == "Memory" {
			t.Fatal("scratch must be size-limited and disk-backed")
		}
		if role == "api" && (volumes["cache"] != nil || mounts["prepared"] != nil) {
			t.Fatal("API owns backend model storage")
		}
		if role == "backend" {
			if cache, ok := values["cache"].(map[string]any); ok && cache["existingClaim"] != "" && cache["existingClaim"] != nil {
				if spec["volumeClaimTemplates"] != nil ||
					object(t, volumes["cache"]["persistentVolumeClaim"])["claimName"] != cache["existingClaim"] {
					t.Fatal("existing cache claim was replaced or adopted")
				}
			} else if spec["volumeClaimTemplates"] == nil {
				t.Fatal("generated cache claims missing")
			}
			if prepared, ok := values["prepared"].(map[string]any); ok {
				claim := object(t, volumes["prepared"]["persistentVolumeClaim"])
				if claim["claimName"] != prepared["existingClaim"] ||
					claim["readOnly"] != true ||
					mounts["prepared"]["readOnly"] != true {
					t.Fatal("prepared claim must remain read-only")
				}
			}
		}
		if images, ok := values["images"].(map[string]any); ok {
			if selected, ok := images[role].(map[string]any); ok {
				actual := container["image"].(string)
				if selected["digest"] != nil && !strings.HasSuffix(actual, "@"+selected["digest"].(string)) {
					t.Fatal("immutable image digest ignored")
				}
				if selected["repository"] != nil && !strings.HasPrefix(actual, selected["repository"].(string)+":") {
					t.Fatal("image repository override ignored")
				}
			}
		}

		selector := object(t, object(t, spec["selector"])["matchLabels"])
		if len(selector) != 3 ||
			selector["helm.sh/chart"] != nil ||
			selector["checksum/config"] != nil ||
			selector["app.kubernetes.io/version"] != nil {
			t.Fatal("unstable StatefulSet selector")
		}
	}

	ingressCount := 0
	for _, resource := range resources {
		if resource["kind"] == "Ingress" {
			ingressCount++
			for _, rule := range object(t, resource["spec"])["rules"].([]any) {
				for _, path := range object(t, object(t, rule)["http"])["paths"].([]any) {
					service := object(t, object(t, object(t, path)["backend"])["service"])
					if !strings.HasSuffix(service["name"].(string), "-api") {
						t.Fatal("Ingress exposes private backend")
					}
				}
			}
		}
	}

	expected := 0
	if ingress, ok := values["ingress"].(map[string]any); ok && ingress["enabled"] == true {
		expected = 1
	}
	if ingressCount != expected {
		t.Fatal("Ingress enablement changed")
	}
}

func TestChartPackageExclusions(t *testing.T) {
	for _, helm := range chartTools(t) {
		copy := filepath.Join(t.TempDir(), "chart")
		if err := os.CopyFS(copy, os.DirFS(filepath.Join(root(t), chartPath()))); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(copy, ".local"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{".local/private.txt", ".env", "credential.key"} {
			if err := os.WriteFile(filepath.Join(copy, path), []byte(privateMarker), 0600); err != nil {
				t.Fatal(err)
			}
		}

		output := t.TempDir()
		run(t, nil, helm, "package", copy, "--destination", output)
		files, err := filepath.Glob(filepath.Join(output, "*.tgz"))
		if err != nil || len(files) != 1 {
			t.Fatal("missing exclusion-check archive")
		}
		verifyChartArchive(t, files[0])
	}
}

func TestChartConfigurationParity(t *testing.T) {
	for _, helm := range chartTools(t) {
		if !bytes.Equal(
			artifact(t, "internal/platform/config/defaults.json"),
			artifact(t, filepath.Join(chartPath(), "files/defaults.json"))) {
			t.Fatal("packaged canonical defaults differ from Go/Docker defaults")
		}

		base := chartRender(t, helm, "1.35.0", nil)
		got, _ := chartConfig(t, base)
		want, err := config.Load(nil, []string{})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("chart default configuration drift")
		}

		changed := chartRender(t, helm, "1.35.0", map[string]any{
			"config": map[string]any{
				"logging": map[string]any{"level": "debug"},
				"api": map[string]any{
					"port": 18080,
					"rateLimit": map[string]any{
						"requestsPerSecond": 2,
						"burst":             6,
					},
					"clientIP": map[string]any{
						"source":            "x-forwarded-for",
						"trustedProxyCIDRs": []string{"10.20.0.0/16", "2001:db8::/32"},
					},
				},
				"backend": map[string]any{
					"port":       18081,
					"nativePort": 18082,
				},
			},
		})

		effective, _ := chartConfig(t, changed)
		if effective.API.Port != 18080 ||
			effective.Backend.Port != 18081 ||
			effective.API.RateLimit.RequestsPerSecond != 2 ||
			effective.Logging.Level != "debug" {
			t.Fatal("override lost")
		}
		for _, role := range []string{"api", "backend"} {
			checksum := func(resources []map[string]any) any {
				return object(
					t, object(
						t, object(
							t, object(
								t, chartObject(t, resources, "StatefulSet", "-"+role)["spec"],
							)["template"],
						)["metadata"],
					)["annotations"],
				)["checksum/config"]
			}
			if checksum(base) == checksum(changed) {
				t.Fatal("configuration change does not restart both controllers")
			}
		}
	}
}

func TestChartInvalidValues(t *testing.T) {
	for _, helm := range chartTools(t) {
		for _, values := range []map[string]any{
			{"unknown": true}, {"replicaCount": 0}, {
				"config": map[string]any{"mode": "ram-cache"},
			},
			{
				"backend": map[string]any{
					"gpu": map[string]any{"requests": 0},
				},
			},
			{
				"backend": map[string]any{
					"gpu": map[string]any{
						"requests": 1,
						"limits":   2,
					},
				},
			},
			{
				"api": map[string]any{
					"resources": map[string]any{
						"limits": map[string]any{"nvidia.com/gpu": 1},
					},
				},
			},
			{"terminationGracePeriodSeconds": 1},
			{
				"probes": map[string]any{"periodSeconds": 1},
				"config": map[string]any{
					"timeouts": map[string]any{
						"provisioningSeconds": 2147483647,
						"operationSeconds":    2147483647,
						"cleanupSeconds":      2147483647,
					},
				},
			},
			{
				"replicaCount": 2,
				"cache":        map[string]any{"existingClaim": "operator-cache"},
			},
			{
				"config": map[string]any{
					"api": map[string]any{
						"rateLimit": map[string]any{"store": "redis"},
					},
				},
			},
			{
				"config": map[string]any{
					"api": map[string]any{
						"rateLimit": map[string]any{
							"requestsPerSecond": 0.1,
							"burst":             10,
							"idleSeconds":       1,
						},
					},
				},
			},
			{
				"config": map[string]any{
					"api": map[string]any{
						"clientIP": map[string]any{"source": "x-forwarded-for"},
					},
				},
			},
			{
				"config": map[string]any{
					"api": map[string]any{
						"clientIP": map[string]any{
							"trustedProxyCIDRs": []string{"0.0.0.0/0"},
						},
					},
				},
			},
			{
				"config": map[string]any{
					"backend": map[string]any{"nativePort": 8081},
				},
			},
			{
				"config": map[string]any{
					"models": map[string]any{"served": "missing"},
				},
			},
			{
				"config": map[string]any{
					"models": map[string]any{
						"downloadSet": []string{"base", "base"},
					},
				},
			},
			{
				"config": map[string]any{
					"api": map[string]any{
						"clientIP": map[string]any{
							"source":            "x-forwarded-for",
							"trustedProxyCIDRs": []string{"1:2:3/64"},
						},
					},
				},
			},
			{
				"config": map[string]any{
					"api": map[string]any{
						"clientIP": map[string]any{
							"source":            "x-forwarded-for",
							"trustedProxyCIDRs": []string{"10.0.0.1/24", "10.0.0.2/24"},
						},
					},
				},
			},
			{
				"config": map[string]any{
					"api": map[string]any{
						"clientIP": map[string]any{
							"source":            "x-forwarded-for",
							"trustedProxyCIDRs": []string{"2001:db8::1/32", "2001:db8::2/32"},
						},
					},
				},
			},
			{
				"config": map[string]any{
					"api": map[string]any{
						"clientIP": map[string]any{
							"source":            "x-forwarded-for",
							"trustedProxyCIDRs": []string{"0:0:0:0:0:ffff:c000:200/128"},
						},
					},
				},
			},
		} {
			args := chartArgs(t, "1.35.0", values)
			cmd := exec.Command(helm, args...)
			cmd.Dir = root(t)
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("invalid values were accepted: %v", values)
			}
			if !bytes.Contains(output, []byte("Error:")) {
				t.Fatalf("invalid-values check failed outside Helm validation: %v", err)
			}
		}
	}
}

func TestChartLabelStrings(t *testing.T) {
	for _, helm := range chartTools(t) {
		for _, tc := range []struct {
			name, release, override string
		}{
			{"numeric-name", "whisper", "123"},
			{"boolean-name", "whisper", "on"},
			{"boolean-release", "true", "whisper-server"},
		} {
			t.Run(filepath.Base(helm)+"/"+tc.name, func(t *testing.T) {
				args := chartArgs(t, "1.35.0", map[string]any{"nameOverride": tc.override})
				args[1] = tc.release
				manifest := run(t, nil, helm, args...)
				python := pythonBinary()
				run(t, manifest, python, "-c", `import sys,yaml
expected={'app.kubernetes.io/name':sys.argv[1], 'app.kubernetes.io/instance':sys.argv[2]}
seen={key:0 for key in expected}
def check(value):
 if isinstance(value,dict):
  for key,nested in value.items():
   if key in expected:
    assert isinstance(nested,str) and nested==expected[key], (key,nested)
    seen[key]+=1
   check(nested)
 elif isinstance(value,list):
  for nested in value: check(nested)
check(list(yaml.safe_load_all(sys.stdin)))
assert all(seen.values()), seen
`, tc.override, tc.release)
			})
		}
	}
}

func TestChartCacheMountPaths(t *testing.T) {
	for _, helm := range chartTools(t) {
		for _, path := range []string{
			"/etc/whisper", "/etc/whisper/", "/etc/./whisper", "/models/../etc/whisper",
			"/etc", "/etc/whisper/cache", "/tmp", "/tmp/cache", "/prepared", "/prepared/cache",
		} {
			t.Run(filepath.Base(helm)+"/reject"+path, func(t *testing.T) {
				args := chartArgs(t, "1.35.0", map[string]any{
					"config": map[string]any{
						"storage": map[string]any{"cacheDir": path},
					},
				})

				cmd := exec.Command(helm, args...)
				cmd.Dir = root(t)
				output, err := cmd.CombinedOutput()
				if err == nil || !bytes.Contains(output, []byte("cache must be separate from")) {
					t.Fatalf("cache mount conflict was not rejected: %s (%v): %s", path, err, output)
				}
			})
		}
		for _, path := range []string{"/models", "/var/cache/whisper", "/etc/whisper-cache", "/tmp-cache", "/prepared-models"} {
			t.Run(filepath.Base(helm)+"/accept"+path, func(t *testing.T) {
				values := map[string]any{
					"config": map[string]any{
						"storage": map[string]any{"cacheDir": path},
					},
				}

				resources := chartRender(t, helm, "1.35.0", values)
				settings, _ := chartConfig(t, resources)
				if settings.Storage.CacheDir != path {
					t.Fatal("cache path override lost")
				}
				checkChartStorageAndProbes(t, resources, settings, values)
			})
		}
	}
}

func TestChartProxyPrefixParity(t *testing.T) {
	for _, helm := range chartTools(t) {
		for _, prefixes := range [][]string{
			{"10.20.0.1/16", "2001:0db8:0:0:0:0:0:1/32", "64:ff9b::192.0.2.1/96"},
			{"10.0.0.0/01"}, {"192.0.2.1/31"}, {"2001:db8::1/127"},
			{"10.0.0.1/24", "10.0.0.2/24"}, {"2001:db8::1/32", "2001:db8::2/32"},
			{"1:2:3/64"}, {"1:2:3:4:5:6:7:8:9/64"}, {"::ffff:192.0.2.0/128"},
			{"::/0"}, {"1.2.3.4/33"}, {"::1%zone/128"},
		} {
			expected := (config.ClientIP{
				Source:            "x-forwarded-for",
				TrustedProxyCIDRs: prefixes,
			}).Validate()
			args := chartArgs(t, "1.35.0", map[string]any{
				"config": map[string]any{
					"api": map[string]any{
						"clientIP": map[string]any{
							"source":            "x-forwarded-for",
							"trustedProxyCIDRs": prefixes,
						},
					},
				},
			})

			cmd := exec.Command(helm, args...)
			cmd.Dir = root(t)
			output, err := cmd.CombinedOutput()
			if (err == nil) != (expected == nil) {
				t.Fatalf(
					"Helm/Go trusted-prefix validation disagrees for %v (Go: %v; Helm: %v): %s",
					prefixes, expected, err, output[:min(len(output), 1024)])
			}
		}
	}
}

func TestChartBoundaries(t *testing.T) {
	for _, helm := range chartTools(t) {
		resources := chartRender(t, helm, "1.37.0", map[string]any{"replicaCount": 2})
		settings, _ := chartConfig(t, resources)
		for _, role := range []string{"api", "backend"} {
			sts := chartObject(t, resources, "StatefulSet", "-"+role)
			spec := object(t, sts["spec"])
			pod := object(t, object(t, spec["template"])["spec"])
			if spec["podManagementPolicy"] != "OrderedReady" ||
				object(t, spec["updateStrategy"])["type"] != "RollingUpdate" ||
				pod["automountServiceAccountToken"] != false {
				t.Fatal("rollout or token boundary changed")
			}
			if pod["terminationGracePeriodSeconds"].(float64) <
				float64(settings.Timeouts.ShutdownSeconds+settings.Timeouts.CleanupSeconds+5) {
				t.Fatal("short termination grace")
			}

			container := object(t, pod["containers"].([]any)[0])
			security := object(t, container["securityContext"])
			if security["readOnlyRootFilesystem"] != true ||
				security["allowPrivilegeEscalation"] != false ||
				object(t, security["capabilities"])["drop"].([]any)[0] != "ALL" {
				t.Fatal("container privilege boundary changed")
			}

			limits := object(t, object(t, container["resources"])["limits"])
			if role == "api" {
				if limits["nvidia.com/gpu"] != nil {
					t.Fatal("GPU allocated to API")
				}

				command := container["command"].([]any)
				script := command[len(command)-1].(string)
				for ordinal := range 2 {
					name := object(t, sts["metadata"])["name"].(string)
					args := string(run(
						t, nil, "env", fmt.Sprintf("POD_NAME=%s-%d", name, ordinal), "/bin/sh", "-ec",
						strings.Replace(script, "exec /usr/local/bin/whisper-api", "printf '%s\\n'", 1)))
					want := fmt.Sprintf(
						"http://%s-backend-%d:%d", strings.TrimSuffix(name, "-api"), ordinal,
						settings.Backend.Port)
					if !strings.Contains(args, "--backend-url\n"+want+"\n") {
						t.Fatal("API does not select its own ordinal backend")
					}
				}
			} else {
				if fmt.Sprint(limits["nvidia.com/gpu"]) != "1" ||
					fmt.Sprint(object(t, object(t, container["resources"])["requests"])["nvidia.com/gpu"]) != "1" {
					t.Fatal("backend GPU missing or mismatched")
				}

				retention := object(t, spec["persistentVolumeClaimRetentionPolicy"])
				if retention["whenDeleted"] != "Retain" || retention["whenScaled"] != "Retain" {
					t.Fatal("cache retention changed")
				}
				if object(t, container["livenessProbe"])["exec"] == nil {
					t.Fatal("backend liveness can restart healthy long inference or recovery")
				}
			}
		}

		count := 0
		for _, resource := range resources {
			if resource["kind"] != "NetworkPolicy" {
				continue
			}
			count++
			spec := object(t, resource["spec"])
			selected := object(t, object(t, spec["podSelector"])["matchLabels"])["statefulset.kubernetes.io/pod-name"].(string)
			rules := spec["ingress"].([]any)
			if len(rules) != 1 {
				t.Fatal("widened ingress rules")
			}

			rule := object(t, rules[0])
			peers := rule["from"].([]any)
			if len(peers) != 1 {
				t.Fatal("widened ingress peers")
			}

			peer := object(t, peers[0])
			if len(peer) != 1 {
				t.Fatal("cross-namespace or IP ingress")
			}

			allowed := object(t, object(t, peer["podSelector"])["matchLabels"])["statefulset.kubernetes.io/pod-name"]
			if allowed != strings.Replace(selected, "-backend-", "-api-", 1) {
				t.Fatal("non-paired API allowed")
			}

			ports := rule["ports"].([]any)
			if len(ports) != 1 || object(t, ports[0])["port"] != float64(settings.Backend.Port) {
				t.Fatal("native or other backend port admitted")
			}

			service := chartObject(t, resources, "Service", strings.TrimPrefix(selected, "whisper-whisper-server"))
			if object(t, object(t, service["spec"])["selector"])["statefulset.kubernetes.io/pod-name"] != selected {
				t.Fatal("shared backend service")
			}
		}
		if count != 2 {
			t.Fatal("per-pair NetworkPolicy missing")
		}
		for _, resource := range resources {
			if slices.Contains(
				[]string{"Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding", "Job", "PersistentVolumeClaim"},
				resource["kind"].(string)) {
				t.Fatal("operator resources or hooks must not be owned")
			}
		}
	}
}
