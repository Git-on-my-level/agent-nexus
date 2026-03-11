package importer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	textExtensions = map[string]struct{}{
		".md": {}, ".markdown": {}, ".txt": {}, ".rst": {}, ".org": {}, ".adoc": {}, ".csv": {}, ".tsv": {},
		".json": {}, ".yaml": {}, ".yml": {}, ".xml": {}, ".html": {}, ".htm": {}, ".css": {}, ".js": {}, ".mjs": {},
		".cjs": {}, ".ts": {}, ".tsx": {}, ".jsx": {}, ".py": {}, ".rb": {}, ".go": {}, ".rs": {}, ".java": {}, ".kt": {},
		".swift": {}, ".sh": {}, ".bash": {}, ".zsh": {}, ".ps1": {}, ".sql": {}, ".toml": {}, ".ini": {}, ".cfg": {},
		".conf": {}, ".env": {}, ".gitignore": {}, ".dockerignore": {}, ".makefile": {}, ".mk": {},
	}
	codeExtensions = map[string]struct{}{
		".py": {}, ".js": {}, ".mjs": {}, ".cjs": {}, ".ts": {}, ".tsx": {}, ".jsx": {}, ".java": {}, ".go": {}, ".rs": {},
		".c": {}, ".cc": {}, ".cpp": {}, ".h": {}, ".hpp": {}, ".rb": {}, ".php": {}, ".swift": {}, ".kt": {}, ".scala": {},
		".sh": {}, ".bash": {}, ".zsh": {}, ".ps1": {}, ".lua": {}, ".r": {}, ".dart": {}, ".cs": {},
	}
	binaryLikeExtensions = map[string]struct{}{
		".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".svg": {}, ".pdf": {}, ".docx": {}, ".pptx": {},
		".xlsx": {}, ".xls": {}, ".doc": {}, ".ppt": {}, ".zip": {}, ".tar": {}, ".gz": {}, ".7z": {}, ".rar": {},
		".mp3": {}, ".wav": {}, ".mp4": {}, ".mov": {}, ".avi": {}, ".heic": {}, ".pages": {}, ".numbers": {},
	}
	ignoreDirs = map[string]struct{}{
		".git": {}, ".svn": {}, ".hg": {}, "node_modules": {}, ".venv": {}, "venv": {}, "dist": {}, "build": {},
		"coverage": {}, ".next": {}, ".nuxt": {}, ".cache": {}, "__pycache__": {}, ".idea": {}, ".DS_Store": {},
	}
	ignoreFilePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i).*\.min\.(js|css)$`),
		regexp.MustCompile(`(?i).*package-lock\.json$`),
		regexp.MustCompile(`(?i).*pnpm-lock\.yaml$`),
		regexp.MustCompile(`(?i).*yarn\.lock$`),
	}
	headingRE = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)
)

// Scan expands a directory or zip archive into a normalized inventory and summary.
func Scan(opts ScanOptions) (ScanSummary, error) {
	if strings.TrimSpace(opts.InputPath) == "" {
		return ScanSummary{}, fmt.Errorf("input path is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return ScanSummary{}, fmt.Errorf("out dir is required")
	}
	if opts.MaxPreviewBytes <= 0 {
		opts.MaxPreviewBytes = 256_000
	}
	if opts.MaxTextCacheBytes <= 0 {
		opts.MaxTextCacheBytes = 2_000_000
	}

	inputPath, err := filepath.Abs(opts.InputPath)
	if err != nil {
		return ScanSummary{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return ScanSummary{}, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return ScanSummary{}, err
	}

	scanRoot, extractedRoot, err := expandInput(inputPath, outDir)
	if err != nil {
		return ScanSummary{}, err
	}
	repoRoots, err := detectRepoRoots(scanRoot)
	if err != nil {
		return ScanSummary{}, err
	}

	records := make([]InventoryRecord, 0, 64)
	countsByCategory := map[string]int{}
	countsByCluster := map[string]int{}
	textCacheDir := filepath.Join(outDir, "text-cache")

	files, err := iterFiles(scanRoot)
	if err != nil {
		return ScanSummary{}, err
	}
	for idx, path := range files {
		sourceID := fmt.Sprintf("src_%06d", idx+1)
		rec, buildErr := buildRecord(sourceID, path, scanRoot, repoRoots, opts.MaxPreviewBytes, textCacheDir, opts.MaxTextCacheBytes)
		if buildErr != nil {
			rel, _ := filepath.Rel(scanRoot, path)
			rec = InventoryRecord{SourceID: sourceID, RelPath: filepath.ToSlash(rel), AbsPath: path, Error: fmt.Sprintf("scan_failed: %v", buildErr)}
		}
		records = append(records, rec)
		category := rec.Category
		if category == "" {
			category = "unknown"
		}
		cluster := rec.ClusterHint
		if cluster == "" {
			cluster = "unknown"
		}
		countsByCategory[category]++
		countsByCluster[cluster]++
	}

	inventoryPath := filepath.Join(outDir, "inventory.jsonl")
	if err := writeInventory(inventoryPath, records); err != nil {
		return ScanSummary{}, err
	}

	relRepoRoots := make([]string, 0, len(repoRoots))
	for _, root := range repoRoots {
		if root == scanRoot {
			relRepoRoots = append(relRepoRoots, ".")
			continue
		}
		rel, _ := filepath.Rel(scanRoot, root)
		relRepoRoots = append(relRepoRoots, filepath.ToSlash(rel))
	}
	sort.Strings(relRepoRoots)

	summary := ScanSummary{
		CreatedAt:           utcNow(),
		Input:               inputPath,
		ScanRoot:            scanRoot,
		ExtractedRoot:       extractedRoot,
		FileCount:           len(records),
		RepoRoots:           relRepoRoots,
		CountsByCategory:    countsByCategory,
		CountsByClusterHint: countsByCluster,
		Inventory:           inventoryPath,
	}
	if err := writeJSON(filepath.Join(outDir, "scan-summary.json"), summary); err != nil {
		return ScanSummary{}, err
	}
	return summary, nil
}

func expandInput(inputPath string, outDir string) (scanRoot string, extractedRoot string, err error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return inputPath, "", nil
	}
	if strings.EqualFold(filepath.Ext(inputPath), ".zip") {
		extractRoot := filepath.Join(outDir, "extracted", strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
		_ = os.RemoveAll(extractRoot)
		if err := os.MkdirAll(extractRoot, 0o755); err != nil {
			return "", "", err
		}
		if err := extractZip(inputPath, extractRoot); err != nil {
			return "", "", err
		}
		entries, err := os.ReadDir(extractRoot)
		if err != nil {
			return "", "", err
		}
		children := make([]string, 0, len(entries))
		for _, entry := range entries {
			children = append(children, filepath.Join(extractRoot, entry.Name()))
		}
		if len(children) == 1 {
			if stat, err := os.Stat(children[0]); err == nil && stat.IsDir() {
				return children[0], extractRoot, nil
			}
		}
		return extractRoot, extractRoot, nil
	}
	return "", "", fmt.Errorf("input must be a directory or .zip file: %s", inputPath)
}

func extractZip(zipPath string, outDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	rootClean := filepath.Clean(outDir)
	prefix := rootClean + string(os.PathSeparator)
	for _, file := range r.File {
		targetPath := filepath.Join(outDir, file.Name)
		cleanTarget := filepath.Clean(targetPath)
		if cleanTarget != rootClean && !strings.HasPrefix(cleanTarget, prefix) {
			return fmt.Errorf("zip entry escapes target root: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			rc.Close()
			return err
		}
		_ = out.Close()
		_ = rc.Close()
	}
	return nil
}

func detectRepoRoots(root string) ([]string, error) {
	markers := map[string]struct{}{".git": {}, "package.json": {}, "pyproject.toml": {}, "go.mod": {}, "Cargo.toml": {}, "pom.xml": {}}
	found := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root {
				if _, ok := ignoreDirs[d.Name()]; ok {
					return filepath.SkipDir
				}
				if isIgnored(path) {
					return filepath.SkipDir
				}
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil
			}
			names := map[string]struct{}{}
			for _, entry := range entries {
				names[entry.Name()] = struct{}{}
			}
			for marker := range markers {
				if _, ok := names[marker]; ok {
					found = append(found, path)
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(found, func(i, j int) bool {
		return len(strings.Split(found[i], string(os.PathSeparator))) < len(strings.Split(found[j], string(os.PathSeparator)))
	})
	return found, nil
}

func iterFiles(root string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root {
				if _, ok := ignoreDirs[d.Name()]; ok {
					return filepath.SkipDir
				}
				if isIgnored(path) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if isIgnored(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func isIgnored(path string) bool {
	base := filepath.Base(path)
	if _, ok := ignoreDirs[base]; ok {
		return true
	}
	candidate := filepath.ToSlash(path)
	for _, re := range ignoreFilePatterns {
		if re.MatchString(candidate) {
			return true
		}
	}
	return false
}

func buildRecord(sourceID string, path string, root string, repoRoots []string, maxPreviewBytes int, textCacheDir string, maxTextCacheBytes int64) (InventoryRecord, error) {
	info, err := os.Stat(path)
	if err != nil {
		return InventoryRecord{}, err
	}
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return InventoryRecord{}, err
	}
	relPath = filepath.ToSlash(relPath)
	ext := strings.ToLower(filepath.Ext(path))
	mimeGuess := mime.TypeByExtension(ext)
	likelyText := isLikelyText(path)
	preview := ""
	encoding := ""
	normalizedHash := ""
	lineCount := 0
	wordCount := 0
	textCachePath := ""
	fullText := ""
	if likelyText && info.Size() <= maxTextCacheBytes {
		if data, readErr := os.ReadFile(path); readErr == nil {
			if text, enc, ok := decodeText(data); ok {
				fullText = text
				encoding = enc
			}
		}
	}
	if fullText != "" || (likelyText && info.Size() == 0) {
		preview = contentPreview(fullText, maxPreviewBytes)
		normalized := normalizeText(fullText)
		if normalized != "" {
			normalizedHash = sha256Bytes([]byte(normalized))
		}
		lineCount = strings.Count(fullText, "\n") + 1
		wordCount = len(wordRE.FindAllString(fullText, -1))
		if err := os.MkdirAll(textCacheDir, 0o755); err != nil {
			return InventoryRecord{}, err
		}
		textCachePath = filepath.Join(textCacheDir, sourceID+".txt")
		if err := os.WriteFile(textCachePath, []byte(fullText), 0o644); err != nil {
			return InventoryRecord{}, err
		}
	} else if likelyText && info.Size() <= int64(maxPreviewBytes*8) {
		if data, readErr := os.ReadFile(path); readErr == nil {
			previewData := data
			if len(previewData) > maxPreviewBytes {
				previewData = previewData[:maxPreviewBytes]
			}
			if text, enc, ok := decodeText(previewData); ok {
				preview = text
				encoding = enc
				normalized := normalizeText(text)
				if normalized != "" {
					normalizedHash = sha256Bytes([]byte(normalized))
				}
				lineCount = strings.Count(text, "\n") + 1
				wordCount = len(wordRE.FindAllString(text, -1))
			}
		}
	}
	category := guessCategory(path, relPath, preview)
	repoRoot := repoRootFor(path, repoRoots)
	title := extractTitle(relPath, preview)
	sha, err := sha256File(path)
	if err != nil {
		return InventoryRecord{}, err
	}
	record := InventoryRecord{
		SourceID:             sourceID,
		RelPath:              relPath,
		AbsPath:              path,
		Filename:             filepath.Base(path),
		Extension:            ext,
		SizeBytes:            info.Size(),
		MTime:                info.ModTime().UTC().Truncate(time.Second).Format(time.RFC3339),
		SHA256:               sha,
		MIMEGuess:            mimeGuess,
		IsText:               likelyText,
		Encoding:             encoding,
		Category:             category,
		RepoRoot:             repoRoot,
		ClusterHint:          guessCluster(relPath, category, repoRoot),
		TitleHint:            title,
		Preview:              contentPreview(preview, 500),
		TextCachePath:        textCachePath,
		NormalizedTextSHA256: normalizedHash,
		LineCount:            lineCount,
		WordCount:            wordCount,
	}
	if _, err := os.Stat(filepath.Join(root, ".obsidian")); err == nil && ext == ".md" {
		record.SourceHints = []string{"obsidian-note"}
	} else if category == "markdown" {
		record.SourceHints = []string{"authored-text"}
	} else if category == "code" {
		record.SourceHints = []string{"code-file"}
	}
	return record, nil
}

func decodeText(data []byte) (string, string, bool) {
	if len(data) == 0 {
		return "", "utf-8", true
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", "", false
	}
	if utf8Valid(data) {
		return string(data), "utf-8", true
	}
	trimmed := strings.ToValidUTF8(string(data), "")
	if trimmed == "" {
		return "", "", false
	}
	return trimmed, "utf-8-ignore", true
}

func utf8Valid(data []byte) bool {
	return utf8.Valid(data)
}

func extractTitle(relPath string, preview string) string {
	if preview != "" {
		if match := headingRE.FindStringSubmatch(preview); len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
		for _, line := range strings.Split(preview, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if len(trimmed) <= 120 && !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "<") {
				return trimmed
			}
		}
	}
	stem := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath)), "_", " "), "-", " "))
	if stem != "" {
		return stem
	}
	return relPath
}

func guessCategory(path string, relPath string, preview string) string {
	ext := strings.ToLower(filepath.Ext(path))
	relLower := strings.ToLower(relPath)
	if _, ok := map[string]struct{}{".md": {}, ".markdown": {}, ".org": {}, ".rst": {}, ".adoc": {}}[ext]; ok {
		return "markdown"
	}
	if _, ok := codeExtensions[ext]; ok {
		return "code"
	}
	if ext == ".json" {
		if strings.Contains(relLower, "slack") || strings.Contains(relLower, "channels.json") || strings.Contains(relLower, "users.json") || strings.Contains(relLower, "messages.json") {
			return "slack-json"
		}
		if strings.Contains(relLower, "trello") || strings.Contains(relLower, "board") {
			return "trello-json"
		}
		return "json"
	}
	if _, ok := map[string]struct{}{".yaml": {}, ".yml": {}, ".toml": {}, ".ini": {}, ".cfg": {}, ".conf": {}}[ext]; ok {
		return "config"
	}
	if _, ok := map[string]struct{}{".csv": {}, ".tsv": {}}[ext]; ok {
		return "table"
	}
	if _, ok := map[string]struct{}{".txt": {}, ".rtf": {}}[ext]; ok {
		return "text"
	}
	if _, ok := map[string]struct{}{".html": {}, ".htm": {}}[ext]; ok {
		return "html"
	}
	if _, ok := map[string]struct{}{".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".svg": {}, ".heic": {}}[ext]; ok {
		return "image"
	}
	if ext == ".pdf" {
		return "pdf"
	}
	if _, ok := map[string]struct{}{".doc": {}, ".docx": {}, ".ppt": {}, ".pptx": {}, ".xls": {}, ".xlsx": {}, ".pages": {}, ".numbers": {}}[ext]; ok {
		return "office"
	}
	if _, ok := map[string]struct{}{".zip": {}, ".tar": {}, ".gz": {}, ".7z": {}, ".rar": {}}[ext]; ok {
		return "archive"
	}
	if preview != "" {
		return "text"
	}
	return "binary"
}

func guessCluster(relPath string, category string, repoRoot string) string {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	if repoRoot != "" {
		return "repo:" + repoRoot
	}
	if category == "slack-json" || category == "trello-json" {
		if len(parts) >= 2 {
			return "export:" + parts[0]
		}
		return "export:" + category
	}
	if len(parts) >= 2 {
		return "dir:" + parts[0]
	}
	return "root"
}

func repoRootFor(path string, repoRoots []string) string {
	best := ""
	for _, candidate := range repoRoots {
		if path == candidate || strings.HasPrefix(path, candidate+string(os.PathSeparator)) {
			if len(candidate) > len(best) {
				best = candidate
			}
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Base(best)
}

func isLikelyText(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := textExtensions[ext]; ok {
		return true
	}
	if _, ok := binaryLikeExtensions[ext]; ok {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 2048)
	n, _ := f.Read(buf)
	buf = buf[:n]
	for _, b := range buf {
		if b == 0 {
			return false
		}
	}
	_, _, ok := decodeText(buf)
	return ok
}
