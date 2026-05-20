package revision

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/api/drive/v3"
)

type Snapshot struct {
	OriginalID string
	CopyID     string
	CopyURL    string
	CreatedAt  time.Time
}

func CreateSnapshot(ctx context.Context, driveSrv *drive.Service, presentationID string) (*Snapshot, error) {
	file, err := driveSrv.Files.Get(presentationID).Fields("name").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get presentation name: %w", err)
	}

	ts := time.Now()
	backupName := fmt.Sprintf("%s [backup %s]", file.Name, ts.Format("2006-01-02 15:04:05"))

	copied, err := driveSrv.Files.Copy(presentationID, &drive.File{
		Name: backupName,
	}).SupportsAllDrives(true).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to copy presentation: %w", err)
	}

	snap := &Snapshot{
		OriginalID: presentationID,
		CopyID:     copied.Id,
		CopyURL:    fmt.Sprintf("https://docs.google.com/presentation/d/%s/edit", copied.Id),
		CreatedAt:  ts,
	}

	slog.Info("snapshot created", "originalID", presentationID, "snapshotID", copied.Id, "snapshotURL", snap.CopyURL)
	return snap, nil
}

func DeleteSnapshot(ctx context.Context, driveSrv *drive.Service, snapshot *Snapshot) error {
	err := driveSrv.Files.Delete(snapshot.CopyID).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}
	slog.Info("snapshot deleted", "snapshotID", snapshot.CopyID)
	return nil
}
