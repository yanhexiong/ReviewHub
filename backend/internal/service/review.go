package service

import (
	"context"
	"net/mail"
	"strings"

	"github.com/yanhexiong/review-hub/backend/internal/domain"
	"github.com/yanhexiong/review-hub/backend/internal/realtime"
	"github.com/yanhexiong/review-hub/backend/internal/repository"
)

type ReviewService struct {
	store *repository.Store
	hub   *realtime.Hub
}

func NewReviewService(store *repository.Store, hub *realtime.Hub) *ReviewService {
	return &ReviewService{store: store, hub: hub}
}

func (service *ReviewService) ListProjects(ctx context.Context, user domain.User) ([]map[string]any, error) {
	return service.store.ListProjects(ctx, user)
}

func (service *ReviewService) Project(ctx context.Context, projectID string, user domain.User) (map[string]any, error) {
	return service.store.PublicProject(ctx, projectID, user)
}

func (service *ReviewService) Snapshots(ctx context.Context, projectID string, user domain.User) ([]map[string]any, error) {
	if _, err := service.store.ProjectAccess(ctx, projectID, user, "view"); err != nil {
		return nil, err
	}
	return service.store.ListSnapshots(ctx, projectID)
}

func (service *ReviewService) UpdateSnapshotMetadata(ctx context.Context, projectID, snapshotID, label, note string, user domain.User) (map[string]any, error) {
	if _, err := service.store.ProjectAccess(ctx, projectID, user, "manage"); err != nil {
		return nil, err
	}
	label = strings.TrimSpace(label)
	note = strings.TrimSpace(note)
	if len(label) > 100 || len(note) > 500 {
		return nil, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	snapshot, err := service.store.SnapshotByID(ctx, projectID, snapshotID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, domain.NewAPIError(404, "NOT_FOUND", "快照不存在")
	}
	if err := service.store.UpdateSnapshotMetadata(ctx, snapshotID, label, note); err != nil {
		return nil, err
	}
	snapshot, err = service.store.SnapshotByID(ctx, projectID, snapshotID)
	if err != nil {
		return nil, err
	}
	service.store.Audit(ctx, &projectID, &user.ID, "snapshot", snapshotID, "metadata_updated", map[string]string{"label": label, "note": note})
	return repository.PublicSnapshot(snapshot), nil
}

func (service *ReviewService) Collaborators(ctx context.Context, projectID string, user domain.User) (map[string]any, []map[string]any, error) {
	if _, err := service.store.ProjectAccess(ctx, projectID, user, "manage"); err != nil {
		return nil, nil, err
	}
	owner, err := service.store.OwnerSummary(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	collaborators, err := service.store.ListCollaborators(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	return owner, collaborators, nil
}

func (service *ReviewService) SaveCollaborator(ctx context.Context, projectID, email, permission string, user domain.User) (map[string]any, []map[string]any, error) {
	access, err := service.store.ProjectAccess(ctx, projectID, user, "manage")
	if err != nil {
		return nil, nil, err
	}
	email = strings.TrimSpace(email)
	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || parsedEmail.Address != email || len(email) > 254 || (permission != "review" && permission != "manage") {
		return nil, nil, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	target, found, err := service.store.CollaboratorTargetByEmail(ctx, email)
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return nil, nil, domain.NewAPIError(404, "COLLABORATOR_ACCOUNT_NOT_FOUND", "未找到该邮箱对应的账户，请让对方先注册站内账户")
	}
	if target.IsActive == 0 {
		return nil, nil, domain.NewAPIError(400, "ACCOUNT_DISABLED", "该账户已停用，无法添加为协作者")
	}
	if target.ID == access.OwnerID {
		return nil, nil, domain.NewAPIError(400, "COLLABORATOR_OWNER", "项目所有者无需重复添加")
	}
	if permission == "manage" && !access.IsOwner {
		return nil, nil, domain.NewAPIError(403, "FORBIDDEN", "只有项目所有者可以授予项目管理权限")
	}
	if err := service.store.UpsertCollaborator(ctx, projectID, target.ID, permission, user.ID); err != nil {
		return nil, nil, err
	}
	service.store.Audit(ctx, &projectID, &user.ID, "collaborator", target.ID, "upserted", map[string]string{
		"email": target.Email, "permission": permission,
	})
	return service.Collaborators(ctx, projectID, user)
}

func (service *ReviewService) RemoveCollaborator(ctx context.Context, projectID, userID string, user domain.User) error {
	access, err := service.store.ProjectAccess(ctx, projectID, user, "manage")
	if err != nil {
		return err
	}
	collaborator, err := service.store.CollaboratorByID(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if collaborator == nil {
		return domain.NewAPIError(404, "COLLABORATOR_NOT_FOUND", "协作者不存在")
	}
	permission, _ := collaborator["permission"].(string)
	if permission == "manage" && !access.IsOwner {
		return domain.NewAPIError(403, "FORBIDDEN", "只有项目所有者可以移除项目管理者")
	}
	if err := service.store.DeleteCollaborator(ctx, projectID, userID); err != nil {
		return err
	}
	service.store.Audit(ctx, &projectID, &user.ID, "collaborator", userID, "removed", map[string]any{
		"email": collaborator["email"], "permission": permission,
	})
	return nil
}

func (service *ReviewService) ListComments(ctx context.Context, projectID string, user domain.User, filter repository.CommentFilter) ([]map[string]any, error) {
	if _, err := service.store.ProjectAccess(ctx, projectID, user, "view"); err != nil {
		return nil, err
	}
	return service.store.ListComments(ctx, projectID, filter)
}

func (service *ReviewService) CreateComment(ctx context.Context, projectID string, user domain.User, input repository.NewComment) (map[string]any, error) {
	if _, err := service.store.ProjectAccess(ctx, projectID, user, "review"); err != nil {
		return nil, err
	}
	if err := validateNewComment(input); err != nil {
		return nil, err
	}
	belongs, err := service.store.SnapshotBelongsToProject(ctx, input.SnapshotID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, domain.NewAPIError(404, "SNAPSHOT_NOT_FOUND", "所选版本不存在或不属于当前项目")
	}
	comment, err := service.store.CreateComment(ctx, projectID, user.ID, input)
	if err != nil {
		return nil, err
	}
	service.store.Audit(ctx, &projectID, &user.ID, "comment", stringValue(comment["id"]), "created", input)
	service.hub.Publish(domain.ProjectEvent{
		Type:       "comment.created",
		ProjectID:  projectID,
		SnapshotID: input.SnapshotID,
		CommentID:  stringValue(comment["id"]),
		Comment:    comment,
	})
	return comment, nil
}

// CreateShareComment applies the same comment validation and realtime event
// flow as an authenticated reviewer, but uses the dedicated share guest
// account after the share password has been checked by the HTTP layer.
func (service *ReviewService) CreateShareComment(ctx context.Context, share repository.Share, input repository.NewComment) (map[string]any, error) {
	guestID := "share-" + share.ID
	exists, err := service.store.ShareGuestExists(ctx, share.ID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.NewAPIError(500, "SHARE_GUEST_MISSING", "分享访客账户不存在")
	}
	if err := validateNewComment(input); err != nil {
		return nil, err
	}
	belongs, err := service.store.SnapshotBelongsToProject(ctx, input.SnapshotID, share.ProjectID)
	if err != nil {
		return nil, err
	}
	if !belongs || input.SnapshotID != share.SnapshotID {
		return nil, domain.NewAPIError(404, "SNAPSHOT_NOT_FOUND", "所选版本不存在或不属于当前分享链接")
	}
	comment, err := service.store.CreateComment(ctx, share.ProjectID, guestID, input)
	if err != nil {
		return nil, err
	}
	service.store.Audit(ctx, &share.ProjectID, &guestID, "comment", stringValue(comment["id"]), "created_via_share", map[string]any{"shareId": share.ID, "pageNumber": input.PageNumber, "category": input.Category, "priority": input.Priority})
	service.hub.Publish(domain.ProjectEvent{Type: "comment.created", ProjectID: share.ProjectID, SnapshotID: share.SnapshotID, CommentID: stringValue(comment["id"]), Comment: comment})
	return comment, nil
}

func (service *ReviewService) DeleteComment(ctx context.Context, commentID string, user domain.User) (domain.ProjectEvent, error) {
	comment, err := service.store.CommentByID(ctx, commentID)
	if err != nil {
		return domain.ProjectEvent{}, err
	}
	if comment == nil || comment["deleted_at"] != nil {
		return domain.ProjectEvent{}, domain.NewAPIError(404, "NOT_FOUND", "评论不存在")
	}
	projectID := stringValue(comment["project_id"])
	access, err := service.store.ProjectAccess(ctx, projectID, user, "review")
	if err != nil {
		return domain.ProjectEvent{}, err
	}
	if !access.IsOwner && stringValue(comment["author_id"]) != user.ID {
		return domain.ProjectEvent{}, domain.NewAPIError(403, "FORBIDDEN", "只能删除自己创建的批注")
	}
	if err := service.store.SoftDeleteComment(ctx, commentID); err != nil {
		return domain.ProjectEvent{}, err
	}
	service.store.Audit(ctx, &projectID, &user.ID, "comment", commentID, "deleted", map[string]any{"authorId": comment["author_id"]})
	return service.hub.Publish(domain.ProjectEvent{
		Type:       "comment.deleted",
		ProjectID:  projectID,
		SnapshotID: stringValue(comment["snapshot_id"]),
		CommentID:  commentID,
	}), nil
}

func (service *ReviewService) TransitionComment(ctx context.Context, commentID, status string, note *string, user domain.User) (domain.ProjectEvent, error) {
	if status != "resolved" && status != "reopened" && status != "obsolete" {
		return domain.ProjectEvent{}, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	if note != nil && len(*note) > 2000 {
		return domain.ProjectEvent{}, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	comment, err := service.store.CommentByID(ctx, commentID)
	if err != nil {
		return domain.ProjectEvent{}, err
	}
	if comment == nil || comment["deleted_at"] != nil {
		return domain.ProjectEvent{}, domain.NewAPIError(404, "NOT_FOUND", "评论不存在")
	}
	projectID := stringValue(comment["project_id"])
	access, err := service.store.ProjectAccess(ctx, projectID, user, "review")
	if err != nil {
		return domain.ProjectEvent{}, err
	}
	if !access.IsOwner && stringValue(comment["author_id"]) != user.ID {
		return domain.ProjectEvent{}, domain.NewAPIError(403, "FORBIDDEN", "只能管理自己创建的批注")
	}
	if err := service.store.TransitionComment(ctx, commentID, status, note, user.ID); err != nil {
		return domain.ProjectEvent{}, err
	}
	updated, err := service.store.CommentByID(ctx, commentID)
	if err != nil {
		return domain.ProjectEvent{}, err
	}
	service.store.Audit(ctx, &projectID, &user.ID, "comment", commentID, "status_"+status, nil)
	return service.hub.Publish(domain.ProjectEvent{
		Type:       "comment.updated",
		ProjectID:  projectID,
		SnapshotID: stringValue(comment["snapshot_id"]),
		CommentID:  commentID,
		Comment:    updated,
	}), nil
}

func (service *ReviewService) ListReplies(ctx context.Context, commentID string, user domain.User) ([]map[string]any, error) {
	comment, err := service.store.CommentByID(ctx, commentID)
	if err != nil {
		return nil, err
	}
	if comment == nil || comment["deleted_at"] != nil {
		return nil, domain.NewAPIError(404, "NOT_FOUND", "评论不存在")
	}
	if _, err := service.store.ProjectAccess(ctx, stringValue(comment["project_id"]), user, "view"); err != nil {
		return nil, err
	}
	return service.store.ListReplies(ctx, commentID)
}

func (service *ReviewService) CreateReply(ctx context.Context, commentID, content string, user domain.User) (map[string]any, int64, domain.ProjectEvent, error) {
	content = strings.TrimSpace(content)
	if content == "" || len(content) > 10_000 {
		return nil, 0, domain.ProjectEvent{}, domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	comment, err := service.store.CommentByID(ctx, commentID)
	if err != nil {
		return nil, 0, domain.ProjectEvent{}, err
	}
	if comment == nil || comment["deleted_at"] != nil {
		return nil, 0, domain.ProjectEvent{}, domain.NewAPIError(404, "NOT_FOUND", "评论不存在")
	}
	projectID := stringValue(comment["project_id"])
	if _, err := service.store.ProjectAccess(ctx, projectID, user, "review"); err != nil {
		return nil, 0, domain.ProjectEvent{}, err
	}
	reply, err := service.store.CreateReply(ctx, commentID, user.ID, content)
	if err != nil {
		return nil, 0, domain.ProjectEvent{}, err
	}
	count, err := service.store.ReplyCount(ctx, commentID)
	if err != nil {
		return nil, 0, domain.ProjectEvent{}, err
	}
	service.store.Audit(ctx, &projectID, &user.ID, "comment_reply", stringValue(reply["id"]), "created", nil)
	event := service.hub.Publish(domain.ProjectEvent{
		Type:       "reply.created",
		ProjectID:  projectID,
		SnapshotID: stringValue(comment["snapshot_id"]),
		CommentID:  commentID,
		Reply:      reply,
		ReplyCount: count,
	})
	return reply, count, event, nil
}

func (service *ReviewService) Subscribe(ctx context.Context, projectID string, user domain.User) (<-chan domain.ProjectEvent, func(), int64, error) {
	if _, err := service.store.ProjectAccess(ctx, projectID, user, "view"); err != nil {
		return nil, nil, 0, err
	}
	channel, cancel, revision := service.hub.Subscribe(projectID)
	return channel, cancel, revision, nil
}

func (service *ReviewService) SubscribeShare(projectID string) (<-chan domain.ProjectEvent, func(), int64) {
	return service.hub.Subscribe(projectID)
}

func validateNewComment(input repository.NewComment) error {
	if input.SnapshotID == "" || input.PageNumber < 1 || (input.AnchorType != "page_note" && input.AnchorType != "rectangle" && input.AnchorType != "text_selection") || strings.TrimSpace(input.Content) == "" || len(input.Content) > 10_000 || strings.TrimSpace(input.Category) == "" || len(input.Category) > 64 || (input.Priority != "low" && input.Priority != "medium" && input.Priority != "high") {
		return domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	for _, value := range []*float64{input.NormalizedX, input.NormalizedY, input.NormalizedWidth, input.NormalizedHeight} {
		if value != nil && (*value < 0 || *value > 1) {
			return domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
		}
	}
	if (input.SelectedText != nil && len(*input.SelectedText) > 5000) || (input.SelectedTextContext != nil && len(*input.SelectedTextContext) > 5000) {
		return domain.NewAPIError(400, "VALIDATION_ERROR", "提交的信息格式不正确，请检查后重试")
	}
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
