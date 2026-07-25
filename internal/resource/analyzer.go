package resource

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

type Analysis struct {
	Platform      Platform `json:"platform"`
	Kind          Kind     `json:"kind"`
	PackageFormat string   `json:"package_format"`
	PackageID     string   `json:"package_id,omitempty"`
	Name          string   `json:"name,omitempty"`
	Version       string   `json:"version,omitempty"`
	Target        string   `json:"target,omitempty"`
	Evidence      []string `json:"evidence"`
	Payload       []byte   `json:"-"`
}

func Analyze(data []byte) (Analysis, error) {
	if isVelaWatchface(data) {
		payload, packageID, normalized, err := normalizeWatchface(data)
		if err != nil {
			return Analysis{}, err
		}
		evidence := []string{"Vela watchface magic=5aa53412"}
		if normalized {
			evidence = append(evidence, "generated missing watchface id="+packageID)
		}
		return Analysis{Platform: VelaOS, Kind: Watchface, PackageFormat: "vela_watchface", PackageID: packageID, Evidence: evidence, Payload: payload}, nil
	}
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x60, 0x5a, 0x5a, 0x7e}) {
		return Analysis{Platform: VelaOS, Kind: Firmware, PackageFormat: "vela_firmware", Evidence: []string{"Vela firmware magic=605a5a7e"}, Payload: data}, nil
	}
	if len(data) < 4 || !bytes.Equal(data[:4], []byte{'P', 'K', 3, 4}) {
		return Analysis{}, fmt.Errorf("unrecognized resource payload")
	}
	return analyzeZip(data, 0)
}

func analyzeZip(data []byte, depth int) (Analysis, error) {
	if depth > 3 {
		return Analysis{}, fmt.Errorf("nested package is too deep")
	}
	files, err := zipFiles(data)
	if err != nil {
		return Analysis{}, err
	}
	if resourceBin := files["resource.bin"]; files["description.xml"] != nil && files["capability.json"] != nil && resourceBin != nil {
		if !isVelaWatchface(resourceBin) {
			return Analysis{}, fmt.Errorf("MWZ resource.bin is not a Vela watchface")
		}
		var description struct {
			Name    string `xml:"name"`
			Version string `xml:"version"`
			Target  string `xml:"deviceType"`
		}
		_ = xml.Unmarshal(files["description.xml"], &description)
		payload, packageID, normalized, err := normalizeWatchface(resourceBin)
		if err != nil {
			return Analysis{}, err
		}
		evidence := []string{"MWZ project", "embedded Vela watchface"}
		if normalized {
			evidence = append(evidence, "generated missing watchface id="+packageID)
		}
		return Analysis{Platform: VelaOS, Kind: Watchface, PackageFormat: "vela_watchface_project", PackageID: packageID, Name: description.Name, Version: description.Version, Target: description.Target, Evidence: evidence, Payload: payload}, nil
	}
	for _, firmware := range []string{"META/firmware.bin", "META/firmware_sign.bin", "firmware.bin"} {
		if files[firmware] != nil {
			return Analysis{Platform: ZeppOS, Kind: Firmware, PackageFormat: "zepp_firmware", Evidence: []string{"Zepp OS firmware entry " + firmware}, Payload: data}, nil
		}
	}
	if manifest := files["manifest.json"]; manifest != nil {
		var root map[string]any
		if json.Unmarshal(manifest, &root) == nil {
			if zpks, ok := root["zpks"].([]any); ok {
				for _, raw := range zpks {
					entry, _ := raw.(map[string]any)
					name, _ := entry["name"].(string)
					if nested := files[name]; nested != nil {
						result, err := analyzeZip(nested, depth+1)
						if err == nil {
							result.PackageFormat = "zepp_bundle"
							result.Payload = data
							result.Evidence = append(result.Evidence, "manifest.json bundle")
							return result, nil
						}
					}
				}
			}
			if packageID := firstString(root, "package", "packageName", "package_name"); packageID != "" {
				return Analysis{Platform: VelaOS, Kind: QuickApp, PackageFormat: "vela_quickapp", PackageID: packageID, Name: firstString(root, "name", "appName"), Version: firstString(root, "versionName", "version"), Evidence: []string{"quick-app manifest package=" + packageID}, Payload: data}, nil
			}
		}
	}
	if nested := files["device.zip"]; nested != nil {
		result, err := analyzeZip(nested, depth+1)
		if err == nil {
			result.PackageFormat = "zepp_bundle"
			result.Payload = data
			result.Evidence = append(result.Evidence, "device.zip bundle")
			return result, nil
		}
	}
	if appJSON := files["app.json"]; appJSON != nil {
		var root struct {
			App struct {
				AppType string `json:"appType"`
				AppID   any    `json:"appId"`
				AppName string `json:"appName"`
				Version struct {
					Name string `json:"name"`
				} `json:"version"`
			} `json:"app"`
		}
		if err := json.Unmarshal(appJSON, &root); err != nil {
			return Analysis{}, fmt.Errorf("invalid Zepp OS app.json: %w", err)
		}
		kind := ZeppApp
		if root.App.AppType == "watchface" {
			kind = Watchface
		} else if root.App.AppType != "app" {
			return Analysis{}, fmt.Errorf("unsupported Zepp OS appType %q", root.App.AppType)
		}
		id, err := appID(root.App.AppID)
		if err != nil {
			return Analysis{}, err
		}
		return Analysis{Platform: ZeppOS, Kind: kind, PackageFormat: "zepp_package", PackageID: id, Name: root.App.AppName, Version: root.App.Version.Name, Evidence: []string{"Zepp OS app.json appType=" + root.App.AppType}, Payload: data}, nil
	}
	return Analysis{}, fmt.Errorf("unrecognized ZIP resource payload")
}

func zipFiles(data []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(reader.File) > 8192 {
		return nil, fmt.Errorf("ZIP has too many files")
	}
	files := make(map[string][]byte)
	var total uint64
	for _, file := range reader.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		clean := path.Clean(name)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(name, "/") {
			return nil, fmt.Errorf("unsafe ZIP path %q", name)
		}
		if file.UncompressedSize64 > 64<<20 {
			return nil, fmt.Errorf("ZIP entry is too large")
		}
		total += file.UncompressedSize64
		if total > 512<<20 {
			return nil, fmt.Errorf("ZIP expands too large")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			return nil, err
		}
		payload, err := io.ReadAll(io.LimitReader(stream, 64<<20+1))
		stream.Close()
		if err != nil {
			return nil, err
		}
		files[clean] = payload
	}
	return files, nil
}

func isVelaWatchface(data []byte) bool {
	return len(data) >= 0x34 && bytes.Equal(data[:4], []byte{0x5a, 0xa5, 0x34, 0x12})
}
func watchfaceID(data []byte) string {
	if len(data) < 0x34 {
		return ""
	}
	value := data[0x28 : 0x28+12]
	end := bytes.IndexByte(value, 0)
	if end >= 0 {
		value = value[:end]
	}
	id := string(value)
	if id == "" || strings.Trim(id, "0") == "" {
		return ""
	}
	for _, char := range id {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-') {
			return ""
		}
	}
	return id
}

func normalizeWatchface(data []byte) ([]byte, string, bool, error) {
	if id := watchfaceID(data); id != "" {
		return data, id, false, nil
	}
	id, err := randomWatchfaceID()
	if err != nil {
		return nil, "", false, fmt.Errorf("generate watchface id: %w", err)
	}
	payload := bytes.Clone(data)
	clear(payload[0x28 : 0x28+12])
	copy(payload[0x28:0x28+12], id)
	return payload, id, true, nil
}

func randomWatchfaceID() (string, error) {
	random := make([]byte, 13)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	length := 9 + int(random[0]%4)
	result := make([]byte, length)
	result[0] = '1' + random[1]%9
	for index := 1; index < length; index++ {
		result[index] = '0' + random[index+1]%10
	}
	return string(result), nil
}
func firstString(root map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := root[key].(string); ok {
			return value
		}
	}
	return ""
}
func appID(value any) (string, error) {
	switch v := value.(type) {
	case float64:
		return strconv.FormatUint(uint64(v), 10), nil
	case string:
		normalized := strings.TrimSpace(strings.ToLower(v))
		base := 10
		if strings.HasPrefix(normalized, "0x") {
			base = 16
			normalized = normalized[2:]
		}
		id, err := strconv.ParseUint(normalized, base, 32)
		if err == nil {
			return strconv.FormatUint(id, 10), nil
		}
	}
	return "", fmt.Errorf("missing or invalid Zepp OS appId")
}
