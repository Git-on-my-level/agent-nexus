package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

const docsIngestHandleMax = 64

type docsIngestRow struct {
	Path           string `json:"path"`
	Handle         string `json:"handle"`
	Source         string `json:"source"`
	Action         string `json:"action"`
	RevisionNumber int    `json:"revision_number,omitempty"`
	Error          string `json:"error,omitempty"`
}

func (a *App) runDocsIngestCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	leadingPath, args := popLeadingPositional(args)
	flagSet := newSilentFlagSet("docs ingest")
	var sourceFlag, verifiedAtFlag, actorIDFlag trackedString
	var tagsFlag, hostsFlag trackedStrings
	flagSet.Var(&sourceFlag, "source", "URL prefix joined with each relative path")
	flagSet.Var(&tagsFlag, "tags", "Extra tags; `knowledge` is always applied")
	flagSet.Var(&hostsFlag, "hosts", "Host names this knowledge tree applies to")
	flagSet.Var(&verifiedAtFlag, "verified-at", "RFC3339 timestamp when this tree was last verified")
	flagSet.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := flagSet.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := flagSet.Args()
	root := strings.TrimSpace(leadingPath)
	if root == "" && len(positionals) > 0 {
		root = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx docs ingest`")
	}
	if root == "" {
		return nil, errnorm.Usage("invalid_request", "path is required; pass a directory of markdown files")
	}
	sourcePrefix := strings.TrimSpace(sourceFlag.value)
	if sourcePrefix == "" {
		return nil, errnorm.Usage("invalid_request", "`--source` is required; pass a URL prefix joined with each relative path")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "invalid_request", "failed to resolve ingest path", err)
	}
	files, err := listMarkdownFiles(absRoot)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "file_read_failed", "failed to walk ingest path", err)
	}
	if len(files) == 0 {
		return nil, errnorm.Usage("invalid_request", "no markdown files found under the ingest path")
	}

	tags := uniqueStringsInOrder(append([]string{"knowledge"}, splitDocsTags(tagsFlag.values)...))
	hosts := splitDocsTags(hostsFlag.values)
	verifiedAt := strings.TrimSpace(verifiedAtFlag.value)
	actorID := ""
	if resolved, err := resolveActorIDAlias(actorIDFlag.value, cfg); err != nil {
		return nil, err
	} else if resolved != "" {
		actorID = resolved
	}

	rows := make([]docsIngestRow, 0, len(files))
	created, updated, unchanged, skipped, failed := 0, 0, 0, 0, 0
	for _, path := range files {
		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		row := docsIngestRow{
			Path:   rel,
			Handle: ingestHandleFromRelPath(rel),
			Source: joinSourceURL(sourcePrefix, rel),
		}
		raw, readErr := a.readBodyInput(path)
		if readErr != nil {
			row.Action = "failed"
			row.Error = readErr.Error()
			failed++
			rows = append(rows, row)
			continue
		}
		content := string(raw)
		if strings.TrimSpace(content) == "" {
			row.Action = "skipped"
			row.Error = "empty markdown file"
			skipped++
			rows = append(rows, row)
			continue
		}
		title := docsTitleFromMarkdown(content, rel)
		action, revisionNumber, putErr := a.upsertIngestDocument(ctx, cfg, row.Handle, title, row.Source, content, tags, hosts, verifiedAt, actorID)
		if putErr != nil {
			row.Action = "failed"
			row.Error = putErr.Error()
			failed++
			rows = append(rows, row)
			continue
		}
		row.Action = action
		row.RevisionNumber = revisionNumber
		switch action {
		case "created":
			created++
		case "updated":
			updated++
		default:
			unchanged++
		}
		rows = append(rows, row)
	}

	data := map[string]any{
		"root":      absRoot,
		"source":    strings.TrimRight(sourcePrefix, "/"),
		"files":     len(rows),
		"created":   created,
		"updated":   updated,
		"unchanged": unchanged,
		"skipped":   skipped,
		"failed":    failed,
		"documents": rows,
	}
	textLines := make([]string, 0, len(rows)+1)
	for _, row := range rows {
		line := fmt.Sprintf("%s\t%s\tdocument:%s", row.Action, row.Path, row.Handle)
		if row.RevisionNumber > 0 {
			line += fmt.Sprintf("\trevision %d", row.RevisionNumber)
		}
		if row.Error != "" {
			line += "\t" + row.Error
		}
		textLines = append(textLines, line)
	}
	textLines = append(textLines, fmt.Sprintf(
		"ingest %d files: %d created, %d updated, %d unchanged, %d skipped, %d failed",
		len(rows), created, updated, unchanged, skipped, failed,
	))
	result := &commandResult{Data: data, Text: strings.Join(textLines, "\n")}
	if failed > 0 {
		return result, errnorm.New(errnorm.KindRemote, "ingest_failed", fmt.Sprintf("docs ingest failed for %d file(s)", failed))
	}
	return result, nil
}

func (a *App) upsertIngestDocument(
	ctx context.Context,
	cfg config.Resolved,
	handle, title, source, content string,
	tags, hosts []string,
	verifiedAt, actorID string,
) (string, int, error) {
	existing, err := a.loadIngestDocument(ctx, cfg, handle)
	if err != nil && !isRemoteNotFound(err) {
		return "", 0, err
	}
	if err == nil && ingestDocumentUnchanged(existing, title, source, content, tags, hosts, verifiedAt) {
		return "unchanged", existing.revisionNumber, nil
	}
	result, err := a.putIngestDocument(ctx, cfg, handle, title, source, content, tags, hosts, verifiedAt, actorID)
	if err != nil {
		return "", 0, err
	}
	revisionNumber := ingestRevisionNumber(result)
	if existing == nil {
		return "created", revisionNumber, nil
	}
	if revisionNumber == 0 || revisionNumber == existing.revisionNumber {
		return "unchanged", existing.revisionNumber, nil
	}
	return "updated", revisionNumber, nil
}

type ingestDocumentState struct {
	title          string
	source         string
	content        string
	tags           []string
	hosts          []string
	verifiedAt     string
	revisionNumber int
}

func (a *App) loadIngestDocument(ctx context.Context, cfg config.Resolved, handle string) (*ingestDocumentState, error) {
	result, err := a.invokeTypedJSON(ctx, cfg, "docs get", "docs.get", map[string]string{"document_id": handle}, nil, nil)
	if err != nil {
		return nil, err
	}
	body := asMap(asMap(result.Data)["body"])
	document := extractNestedMap(body, "document")
	revision := extractNestedMap(body, "revision")
	content := firstNonEmpty(anyString(revision["content"]), anyString(body["content"]), anyString(body["body_text"]))
	revisionNumber := intValue(revision["revision_number"])
	if revisionNumber == 0 {
		revisionNumber = intValue(document["head_revision_number"])
	}
	return &ingestDocumentState{
		title:          anyString(document["title"]),
		source:         anyString(document["source"]),
		content:        content,
		tags:           stringList(document["tags"]),
		hosts:          stringList(document["hosts"]),
		verifiedAt:     anyString(document["verified_at"]),
		revisionNumber: revisionNumber,
	}, nil
}

func (a *App) putIngestDocument(
	ctx context.Context,
	cfg config.Resolved,
	handle, title, source, content string,
	tags, hosts []string,
	verifiedAt, actorID string,
) (*commandResult, error) {
	document := map[string]any{
		"title":  title,
		"source": source,
		"tags":   tags,
	}
	if len(hosts) > 0 {
		document["hosts"] = hosts
	}
	if verifiedAt != "" {
		document["verified_at"] = verifiedAt
	}
	body := map[string]any{
		"document":     document,
		"content":      content,
		"content_type": "text",
	}
	if actorID != "" {
		body["actor_id"] = actorID
	} else if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "docs put", "docs.put", map[string]string{"document_id": handle}, nil, body)
		if err == nil {
			return addResourceURLToResult(cfg, "docs.put", result), nil
		}
		lastErr = err
		if !isRemoteRateLimited(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func ingestDocumentUnchanged(existing *ingestDocumentState, title, source, content string, tags, hosts []string, verifiedAt string) bool {
	if existing == nil {
		return false
	}
	if existing.title != title || existing.source != source {
		return false
	}
	if !stringSetsEqual(existing.tags, tags) {
		return false
	}
	if len(hosts) > 0 && !stringSetsEqual(existing.hosts, hosts) {
		return false
	}
	if verifiedAt != "" && existing.verifiedAt != verifiedAt {
		return false
	}
	return ingestContentEqual(existing.content, content)
}

func ingestRevisionNumber(result *commandResult) int {
	if result == nil {
		return 0
	}
	data := asMap(result.Data)
	body := asMap(data["body"])
	revision := extractNestedMap(body, "revision")
	if n := intValue(revision["revision_number"]); n > 0 {
		return n
	}
	document := extractNestedMap(body, "document")
	return intValue(document["head_revision_number"])
}

func isRemoteRateLimited(err error) bool {
	var normalized *errnorm.Error
	if !errors.As(err, &normalized) || normalized == nil {
		return false
	}
	return normalized.Code == "rate_limited"
}

func listMarkdownFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if isMarkdownFilename(info.Name()) {
			return []string{root}, nil
		}
		return nil, fmt.Errorf("ingest path is not a markdown file or directory")
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (name == ".git" || name == "node_modules" || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !isMarkdownFilename(name) {
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

func isMarkdownFilename(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

func ingestHandleFromRelPath(rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	rel = strings.TrimPrefix(rel, "./")
	stem := strings.TrimSuffix(rel, filepath.Ext(rel))
	slug := slugifyDocsHandle(stem)
	if slug == "" {
		slug = "kb"
	}
	sum := sha256.Sum256([]byte(rel))
	suffix := hex.EncodeToString(sum[:4])
	room := docsIngestHandleMax - 1 - len(suffix)
	if room < 1 {
		return suffix
	}
	if len(slug) > room {
		slug = strings.Trim(slug[:room], "-")
	}
	if slug == "" {
		slug = "kb"
	}
	return slug + "-" + suffix
}

func joinSourceURL(prefix, rel string) string {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
	rel = strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(rel)), "/")
	if prefix == "" {
		return rel
	}
	if rel == "" {
		return prefix
	}
	return prefix + "/" + rel
}

func docsTitleFromMarkdown(content, rel string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if title != "" {
				return title
			}
		}
		break
	}
	stem := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	if strings.TrimSpace(stem) != "" {
		return stem
	}
	return rel
}

func ingestContentEqual(stored, file string) bool {
	return normalizeIngestContent(stored) == normalizeIngestContent(file)
}

func normalizeIngestContent(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	blankRun := 0
	for _, line := range lines {
		line = strings.TrimRightFunc(line, unicode.IsSpace)
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

func stringSetsEqual(a, b []string) bool {
	left := uniqueStringsInOrder(a)
	right := uniqueStringsInOrder(b)
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, item := range left {
		seen[item] = struct{}{}
	}
	for _, item := range right {
		if _, ok := seen[item]; !ok {
			return false
		}
	}
	return true
}
