package primitives

import "context"

// ReportSubjectSnapshot reads the same scoped canonical row as the full getter.
// Reports need its title and lifecycle, without hydrating child associations.
func (s *Store) ReportSubjectSnapshot(ctx context.Context, kind, id string) (map[string]any, error) {
	switch kind {
	case "board":
		row, err := s.getBoardRow(ctx, id)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"id": row.ID, "title": row.Title, "thread_id": row.ThreadID}
		lifecycleFieldsFromSQLColumns(row.ArchivedAt, row.ArchivedBy, row.TrashedAt, row.TrashedBy, row.TrashReason).apply(out)
		return out, nil
	case "topic":
		row, err := s.getTopicRow(ctx, id)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"id": row.ID, "title": row.Title.String, "thread_id": row.ThreadID.String}
		lifecycleFieldsFromSQLColumns(row.ArchivedAt, row.ArchivedBy, row.TrashedAt, row.TrashedBy, row.TrashReason).apply(out)
		return out, nil
	}
	return nil, ErrInvalidResourceRef
}
