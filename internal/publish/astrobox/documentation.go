package astrobox

import (
	"fmt"
	"path"
	"strings"
)

type downloadEntry struct {
	File    string
	Version string
	SHA256  string
	Devices []deviceRef
}

func paidLabel(paidType string) string {
	if paidType == "" {
		return "免费"
	}
	return paidType
}

func restypeLabel(restype string) string {
	switch restype {
	case "quick_app":
		return "快应用（quick_app）"
	case "watchface":
		return "表盘（watchface）"
	default:
		return restype
	}
}

func deviceLabel(device deviceRef) string {
	if device.Name == "" || device.Name == device.ID {
		return device.ID
	}
	return device.Name + "（" + device.ID + "）"
}

func uniqueDeviceRefs(entries []downloadEntry) []deviceRef {
	seen := map[string]bool{}
	var result []deviceRef
	for _, entry := range entries {
		for _, device := range entry.Devices {
			if !seen[device.ID] {
				seen[device.ID] = true
				result = append(result, device)
			}
		}
	}
	return result
}

func buildREADME(snap snapshot, cfg publishConfig, restype, author, coverPath string, previews []string, entries []downloadEntry) string {
	var output strings.Builder
	fmt.Fprintf(&output, "# %s\n\n", snap.Revision.Name)
	if summary := strings.TrimSpace(snap.Revision.Description); summary != "" {
		fmt.Fprintf(&output, "%s\n\n", summary)
	}
	fmt.Fprintf(&output, "![Cover](%s)\n\n", coverPath)
	output.WriteString("|  |  |\n| --- | --- |\n")
	fmt.Fprintf(&output, "| 资源 ID | `%s` |\n", cfg.ItemID)
	fmt.Fprintf(&output, "| 类型 | %s |\n", restypeLabel(restype))
	fmt.Fprintf(&output, "| 作者 | %s |\n", author)
	fmt.Fprintf(&output, "| 付费类型 | %s |\n", paidLabel(cfg.PaidType))
	if purchaseLink := strings.TrimSpace(snap.Revision.PurchaseLink); purchaseLink != "" {
		fmt.Fprintf(&output, "| 购买链接 | [%s](%s) |\n", purchaseLink, purchaseLink)
	}
	if len(cfg.Tags) > 0 {
		fmt.Fprintf(&output, "| 标签 | %s |\n", strings.Join(cfg.Tags, " / "))
	}
	output.WriteString("\n## 支持设备\n\n")
	for _, device := range uniqueDeviceRefs(entries) {
		fmt.Fprintf(&output, "- %s\n", deviceLabel(device))
	}
	output.WriteString("\n## 下载\n\n| 文件 | 版本 | 设备 |\n| --- | --- | --- |\n")
	for _, entry := range entries {
		devices := make([]string, 0, len(entry.Devices))
		for _, device := range entry.Devices {
			devices = append(devices, deviceLabel(device))
		}
		fmt.Fprintf(&output, "| [%s](%s) | %s | %s |\n", path.Base(entry.File), entry.File, entry.Version, strings.Join(devices, "<br>"))
	}
	if len(previews) > 0 {
		output.WriteString("\n## 预览\n\n")
		for _, preview := range previews {
			fmt.Fprintf(&output, "![Preview](%s)\n", preview)
		}
	}
	output.WriteString("\n---\n\n本仓库由 [OronBox 创作者中心](https://oronbox.zxor.org) 创建和维护。This repository is created and maintained by OronBox Creator Center.\n")
	return output.String()
}

func buildPRBody(snap snapshot, cfg publishConfig, restype string, repo repo, shortCommit, iconPath, coverPath string, previews []string, entries []downloadEntry) string {
	rawURL := func(filePath string) string {
		return "https://raw.githubusercontent.com/" + repo.Owner + "/" + repo.Name + "/refs/heads/" + repo.Branch + "/" + filePath
	}
	var output strings.Builder
	output.WriteString("## 资源信息\n\n")
	fmt.Fprintf(&output, "- 资源名称：%s\n", snap.Revision.Name)
	fmt.Fprintf(&output, "- 资源 ID：%s\n", cfg.ItemID)
	fmt.Fprintf(&output, "- 资源类型：%s\n", restypeLabel(restype))
	output.WriteString("- 提交清单：manifest_v2\n")
	fmt.Fprintf(&output, "- 付费类型：%s\n", paidLabel(cfg.PaidType))
	if purchaseLink := strings.TrimSpace(snap.Revision.PurchaseLink); purchaseLink != "" {
		fmt.Fprintf(&output, "- 购买链接：%s\n", purchaseLink)
	}
	if len(cfg.Tags) > 0 {
		fmt.Fprintf(&output, "- 标签：%s\n", strings.Join(cfg.Tags, " / "))
	}
	output.WriteString("\n## 支持设备\n\n")
	for _, device := range uniqueDeviceRefs(entries) {
		if device.Name == "" || device.Name == device.ID {
			fmt.Fprintf(&output, "- %s\n", device.ID)
		} else {
			fmt.Fprintf(&output, "- %s（device_id: %s）\n", device.Name, device.ID)
		}
	}
	fmt.Fprintf(&output, "\n## 仓库信息\n\n- 资源仓库：https://github.com/%s/%s\n- 提交短哈希：%s\n", repo.Owner, repo.Name, shortCommit)
	output.WriteString("\n## 图片资源\n\n")
	fmt.Fprintf(&output, "- Icon：%s\n  %s\n", iconPath, rawURL(iconPath))
	fmt.Fprintf(&output, "- Cover：%s\n  %s\n", coverPath, rawURL(coverPath))
	if len(previews) > 0 {
		output.WriteString("- Preview：\n")
		for _, preview := range previews {
			fmt.Fprintf(&output, "  - %s\n    %s\n", preview, rawURL(preview))
		}
	}
	output.WriteString("\n## 下载资源（downloads）\n\n")
	for _, entry := range entries {
		fmt.Fprintf(&output, "- %s\n", entry.File)
		fmt.Fprintf(&output, "  - version: %s\n", entry.Version)
		devices := make([]string, 0, len(entry.Devices))
		for _, device := range entry.Devices {
			devices = append(devices, deviceLabel(device))
		}
		fmt.Fprintf(&output, "  - devices: %s\n", strings.Join(devices, "、"))
		fmt.Fprintf(&output, "  - raw: %s\n", rawURL(entry.File))
		fmt.Fprintf(&output, "  - sha256: %s\n", entry.SHA256)
	}
	output.WriteString("\n---\n\n此 PR 由 [OronBox 创作者中心](https://oronbox.zxor.org) 提交。Submitted through OronBox Creator Center.\n")
	return output.String()
}
