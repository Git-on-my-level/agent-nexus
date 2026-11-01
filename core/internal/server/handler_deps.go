// Dependency interfaces the HTTP handler consumes: the primitive store
// facade, the actor registry, and the health check callbacks injected via
// options.

package server

import (
	"context"
	"io"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
)

type HealthCheckFunc func(ctx context.Context) error
type OpsHealthSectionFunc func(ctx context.Context) map[string]any

type namedReadinessCheck struct {
	Name    string
	Code    string
	Message string
	Check   HealthCheckFunc
}

type ActorRegistry interface {
	DisplayNames(ctx context.Context, ids []string) (map[string]string, error)
	Register(ctx context.Context, actor actors.Actor) (actors.Actor, error)
	List(ctx context.Context, filter actors.ActorListFilter) ([]actors.Actor, string, error)
	Exists(ctx context.Context, actorID string) (bool, error)
	Get(ctx context.Context, actorID string) (actors.Actor, error)
}

type PrimitiveStore interface {
	AppendEvent(ctx context.Context, actorID string, event map[string]any) (map[string]any, error)
	GetEvent(ctx context.Context, id string) (map[string]any, error)
	ArchiveEvent(ctx context.Context, actorID, eventID string) (map[string]any, error)
	UnarchiveEvent(ctx context.Context, actorID, eventID string) (map[string]any, error)
	TrashEvent(ctx context.Context, actorID, eventID, reason string) (map[string]any, error)
	RestoreEvent(ctx context.Context, actorID, eventID string) (map[string]any, error)
	CreateArtifact(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) (map[string]any, error)
	CreateArtifactAttachment(ctx context.Context, actorID string, artifact map[string]any, mimeType string, originalFilename string, src io.Reader, maxUploadBytes int64) (map[string]any, error)
	CreateArtifactAndEvent(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string, event map[string]any) (map[string]any, map[string]any, error)
	GetArtifact(ctx context.Context, id string) (map[string]any, error)
	GetArtifactContent(ctx context.Context, id string) ([]byte, string, error)
	GetArtifactContentHTTP(ctx context.Context, id string) (primitives.ArtifactContentHTTP, error)
	UpsertAgentWakeup(ctx context.Context, wakeup primitives.AgentWakeup) (primitives.AgentWakeup, error)
	GetAgentWakeup(ctx context.Context, wakeupID string) (primitives.AgentWakeup, error)
	ListAgentWakeups(ctx context.Context, filter primitives.AgentWakeupListFilter) ([]primitives.AgentWakeup, error)
	ReceiptStreamCursor(ctx context.Context, threadID string, lastEventID string, accept func(primitives.AgentWakeup) bool) (primitives.ReceiptStreamCursor, error)
	ListReceiptStreamPage(ctx context.Context, threadID string, cursor primitives.ReceiptStreamCursor) (primitives.ReceiptStreamPage, error)
	ClaimAgentWakeup(ctx context.Context, wakeupID string, targetActorID string, bridgeInstanceID string) (primitives.AgentWakeup, error)
	CompleteAgentWakeup(ctx context.Context, wakeupID string, targetActorID string, bridgeInstanceID string) (primitives.AgentWakeup, error)
	FailAgentWakeup(ctx context.Context, wakeupID string, targetActorID string, bridgeInstanceID string, reason string) (primitives.AgentWakeup, error)
	MarkAgentWakeupNotification(ctx context.Context, wakeupID string, targetActorID string, status string) (primitives.AgentWakeup, error)
	ListArtifacts(ctx context.Context, filter primitives.ArtifactListFilter) ([]map[string]any, error)
	GetWorkspaceUsageSummary(ctx context.Context) (primitives.WorkspaceUsageSummary, error)
	GetWorkspaceUsageV1Summary(ctx context.Context) (primitives.WorkspaceUsageV1Summary, error)
	RebuildBlobUsageLedger(ctx context.Context) (primitives.BlobUsageLedgerRebuildResult, error)
	GetIdempotencyReplay(ctx context.Context, scope string, actorID string, requestKey string) (primitives.IdempotencyReplay, error)
	PutIdempotencyReplay(ctx context.Context, scope string, actorID string, requestKey string, requestHash string, status int, response map[string]any) error
	ListDerivedInboxItems(ctx context.Context, filter primitives.DerivedInboxListFilter) ([]primitives.DerivedInboxItem, error)
	ReadInbox(ctx context.Context, options primitives.InboxReadOptions) ([]primitives.DerivedInboxItem, int, error)
	ListInboxThreadIDs(ctx context.Context) ([]string, error)
	GetDerivedInboxItem(ctx context.Context, id string) (primitives.DerivedInboxItem, error)
	ReplaceDerivedInboxItems(ctx context.Context, threadID string, items []primitives.DerivedInboxItem, expectedGeneration ...int64) error
	GetDerivedTopicProjection(ctx context.Context, threadID string) (primitives.DerivedTopicProjection, error)
	ListDerivedTopicProjections(ctx context.Context, threadIDs []string) (map[string]primitives.DerivedTopicProjection, error)
	PutDerivedTopicProjection(ctx context.Context, projection primitives.DerivedTopicProjection) error
	MarkDerivedTopicProjectionDirty(ctx context.Context, threadID string, dirtyAt string) error
	ClearDerivedTopicProjectionDirty(ctx context.Context, threadID string) error
	ListDerivedTopicProjectionDirtyEntries(ctx context.Context, limit int, now time.Time) ([]primitives.DerivedTopicProjectionDirtyEntry, error)
	ResolveResourceRef(ctx context.Context, input primitives.ResourceRefInput) (primitives.ResolvedResourceRef, error)
	GetDerivedTopicProjectionQueueStats(ctx context.Context) (primitives.DerivedTopicProjectionQueueStats, error)
	ListDocuments(ctx context.Context, filter primitives.DocumentListFilter) ([]map[string]any, string, error)
	MarkTopicProjectionsDirty(ctx context.Context, threadIDs []string, queuedAt time.Time) error
	RequeueTopicProjectionRefresh(ctx context.Context, threadID string, queuedAt time.Time) error
	GetTopicProjectionRefreshStatuses(ctx context.Context, threadIDs []string) (map[string]primitives.TopicProjectionRefreshStatus, error)
	MarkTopicProjectionRefreshStarted(ctx context.Context, threadID string, startedAt time.Time) (int64, error)
	MarkTopicProjectionRefreshSucceeded(ctx context.Context, threadID string, completedGeneration int64, completedAt time.Time) error
	MarkTopicProjectionRefreshFailed(ctx context.Context, threadID string, failedGeneration int64, failedAt time.Time, message string) error
	CreateDocument(ctx context.Context, actorID string, document map[string]any, content any, contentType string, refs []string) (map[string]any, map[string]any, error)
	GetDocument(ctx context.Context, documentID string) (map[string]any, map[string]any, error)
	PatchDocument(ctx context.Context, actorID string, documentID string, patch map[string]any, ifUpdatedAt *string) (map[string]any, map[string]any, error)
	SearchDocuments(ctx context.Context, filter primitives.DocumentSearchFilter) ([]map[string]any, string, error)
	ListDocumentComments(ctx context.Context, documentID string, limit *int, cursor string) ([]map[string]any, string, error)
	CreateDocumentComment(ctx context.Context, actorID, documentID, text, parentID string) (map[string]any, error)
	UpdateDocumentComment(ctx context.Context, actorID, documentID, commentID, text string) (map[string]any, error)
	DeleteDocumentComment(ctx context.Context, actorID, documentID, commentID string) (map[string]any, error)
	UpdateDocument(ctx context.Context, actorID string, documentID string, documentPatch map[string]any, ifBaseRevision string, content any, contentType string, refs []string, revisionProvenance map[string]any) (map[string]any, map[string]any, error)
	ListDocumentHistory(ctx context.Context, documentID string) ([]map[string]any, error)
	GetDocumentRevision(ctx context.Context, documentID string, revisionID string) (map[string]any, error)
	GetDocumentRevisionByID(ctx context.Context, revisionID string) (map[string]any, error)
	ListBoards(ctx context.Context, filter primitives.BoardListFilter) ([]primitives.BoardListItem, string, error)
	CreateBoard(ctx context.Context, actorID string, board map[string]any) (map[string]any, error)
	GetBoard(ctx context.Context, boardID string) (map[string]any, error)
	GetBoardSummary(ctx context.Context, boardID string) (map[string]any, error)
	UpdateBoard(ctx context.Context, actorID string, boardID string, patch map[string]any, ifUpdatedAt *string) (map[string]any, error)
	ListCards(ctx context.Context, filter primitives.CardListFilter) ([]map[string]any, error)
	InboxThreadAccessOwners(ctx context.Context, threadID string) ([]string, error)
	ListBoardCards(ctx context.Context, boardID string) ([]map[string]any, error)
	GetBoardCard(ctx context.Context, boardID string, identifier string) (map[string]any, error)
	GetBoardCardCurrent(ctx context.Context, boardID string, identifier string) (map[string]any, error)
	CreateBoardCard(ctx context.Context, actorID string, boardID string, input primitives.AddBoardCardInput) (primitives.BoardCardMutationResult, error)
	CreateBoardCardsBatch(ctx context.Context, actorID string, boardID string, ifBoard *string, inputs []primitives.AddBoardCardInput) ([]primitives.BoardCardMutationResult, error)
	AddBoardCard(ctx context.Context, actorID string, boardID string, input primitives.AddBoardCardInput) (primitives.BoardCardMutationResult, error)
	UpdateBoardCard(ctx context.Context, actorID string, boardID string, threadID string, input primitives.UpdateBoardCardInput) (primitives.BoardCardMutationResult, error)
	MoveBoardCard(ctx context.Context, actorID string, boardID string, threadID string, input primitives.MoveBoardCardInput) (primitives.BoardCardMutationResult, error)
	RemoveBoardCard(ctx context.Context, actorID string, boardID string, identifier string, input primitives.RemoveBoardCardInput) (primitives.BoardCardRemovalResult, error)
	ArchiveBoardCard(ctx context.Context, actorID string, boardID string, identifier string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error)
	TrashBoardCard(ctx context.Context, actorID string, boardID string, identifier string, reason string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error)
	RestoreArchivedBoardCard(ctx context.Context, actorID string, boardID string, identifier string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error)
	PurgeArchivedBoardCard(ctx context.Context, boardID string, identifier string) error
	ListBoardCardHistory(ctx context.Context, cardID string) ([]map[string]any, error)
	CreateCardRevision(ctx context.Context, actorID string, cardID string, input primitives.CreateCardRevisionInput) (primitives.BoardCardMutationResult, map[string]any, error)
	ListBoardMembershipsByThread(ctx context.Context, threadID string) ([]primitives.BoardMembership, error)
	ListRefEdgesBySource(ctx context.Context, sourceRef string, relationFilter string) ([]primitives.RefEdge, error)
	ListRefEdgesByTarget(ctx context.Context, targetRef string, relationFilter string) ([]primitives.RefEdge, error)
	ListTopics(ctx context.Context, filter primitives.TopicListFilter) ([]map[string]any, string, error)
	CreateTopic(ctx context.Context, actorID string, topic map[string]any) (primitives.TopicPatchResult, error)
	GetTopic(ctx context.Context, topicID string) (map[string]any, error)
	PatchTopic(ctx context.Context, actorID string, topicID string, patch map[string]any, ifUpdatedAt *string) (primitives.TopicPatchResult, error)
	ArchiveTopic(ctx context.Context, actorID string, topicID string) (map[string]any, error)
	ArchiveTopicIfUpdatedAt(ctx context.Context, actorID string, topicID string, ifUpdatedAt *string) (map[string]any, error)
	UnarchiveTopic(ctx context.Context, actorID string, topicID string) (map[string]any, error)
	TrashTopic(ctx context.Context, actorID string, topicID string, reason string) (map[string]any, error)
	RestoreTopic(ctx context.Context, actorID string, topicID string) (map[string]any, error)
	CreateThread(ctx context.Context, actorID string, thread map[string]any) (primitives.ThreadMutationResult, error)
	GetThread(ctx context.Context, id string) (map[string]any, error)
	PatchThread(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (primitives.ThreadMutationResult, error)
	ListThreads(ctx context.Context, filter primitives.ThreadListFilter) ([]map[string]any, string, error)
	ListEventsByThread(ctx context.Context, threadID string) ([]map[string]any, error)
	BatchCountMessagePostedEventsByThreadIDs(ctx context.Context, threadIDs []string) (map[string]int, error)
	ListRecentEventsByThread(ctx context.Context, threadID string, limit int) ([]map[string]any, error)
	ListEvents(ctx context.Context, filter primitives.EventListFilter) ([]map[string]any, error)
	ListEventsPage(ctx context.Context, filter primitives.EventListFilter) (primitives.EventPage, error)
	EventStreamCursor(ctx context.Context, lastEventID string) (primitives.EventCursor, error)
	ListEventStreamPage(ctx context.Context, filter primitives.EventListFilter, cursor primitives.EventCursor) (primitives.EventStreamPage, error)
	ListHomeUnread(ctx context.Context, readerID string) ([]primitives.HomeUnreadGroup, int, error)
	MarkHomeRead(ctx context.Context, readerID string, groupRefs []string) error
	MarkHomeReadAt(ctx context.Context, readerID string, groupRefs []string, expected map[string]primitives.EventCursor) error
	ListHumanAttentionRespondedPage(ctx context.Context, params primitives.HumanAttentionRespondedPageParams) ([]map[string]any, error)
	TrashArtifact(ctx context.Context, actorID string, artifactID string, reason string) (map[string]any, error)
	ArchiveArtifact(ctx context.Context, actorID string, artifactID string) (map[string]any, error)
	UnarchiveArtifact(ctx context.Context, actorID string, artifactID string) (map[string]any, error)
	RestoreArtifact(ctx context.Context, actorID string, artifactID string) (map[string]any, error)
	PurgeTrashedArtifact(ctx context.Context, artifactID string) error
	TrashDocument(ctx context.Context, actorID string, documentID string, reason string) (map[string]any, map[string]any, error)
	ArchiveDocument(ctx context.Context, actorID string, documentID string) (map[string]any, map[string]any, error)
	ArchiveDocumentIfUpdatedAt(ctx context.Context, actorID string, documentID string, ifUpdatedAt *string) (map[string]any, map[string]any, error)
	UnarchiveDocument(ctx context.Context, actorID string, documentID string) (map[string]any, map[string]any, error)
	RestoreDocument(ctx context.Context, actorID string, documentID string, reason string) (map[string]any, map[string]any, error)
	PurgeDocument(ctx context.Context, documentID string) error
	ArchiveThread(ctx context.Context, actorID string, threadID string) (map[string]any, error)
	UnarchiveThread(ctx context.Context, actorID string, threadID string) (map[string]any, error)
	TrashThread(ctx context.Context, actorID string, threadID string, reason string) (map[string]any, error)
	RestoreThread(ctx context.Context, actorID string, threadID string) (map[string]any, error)
	PurgeThread(ctx context.Context, threadID string) error
	ArchiveBoard(ctx context.Context, actorID string, boardID string) (map[string]any, error)
	ArchiveBoardIfUpdatedAt(ctx context.Context, actorID string, boardID string, ifUpdatedAt *string) (map[string]any, error)
	UnarchiveBoard(ctx context.Context, actorID string, boardID string) (map[string]any, error)
	TrashBoard(ctx context.Context, actorID string, boardID string, reason string) (map[string]any, error)
	RestoreBoard(ctx context.Context, actorID string, boardID string) (map[string]any, error)
	PurgeBoard(ctx context.Context, boardID string) error
}
