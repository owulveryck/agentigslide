package pipeline

import (
	"context"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/slides/v1"
)

// SlidesAPI abstracts the Google Slides API operations used by the pipeline.
type SlidesAPI interface {
	GetPresentation(presentationID string) (*slides.Presentation, error)
	BatchUpdate(presentationID string, req *slides.BatchUpdatePresentationRequest) (*slides.BatchUpdatePresentationResponse, error)
	GetPageThumbnail(presentationID, pageObjectID string) (*slides.Thumbnail, error)
}

// DriveAPI abstracts the Google Drive API operations used by the pipeline.
type DriveAPI interface {
	CopyFile(ctx context.Context, fileID string, file *drive.File) (*drive.File, error)
}

type slidesWrapper struct {
	svc *slides.Service
}

// WrapSlides wraps a *slides.Service to satisfy SlidesAPI.
func WrapSlides(svc *slides.Service) SlidesAPI {
	return &slidesWrapper{svc: svc}
}

func (w *slidesWrapper) GetPresentation(id string) (*slides.Presentation, error) {
	return w.svc.Presentations.Get(id).Do()
}

func (w *slidesWrapper) BatchUpdate(id string, req *slides.BatchUpdatePresentationRequest) (*slides.BatchUpdatePresentationResponse, error) {
	return w.svc.Presentations.BatchUpdate(id, req).Do()
}

func (w *slidesWrapper) GetPageThumbnail(presID, pageID string) (*slides.Thumbnail, error) {
	return w.svc.Presentations.Pages.GetThumbnail(presID, pageID).ThumbnailPropertiesThumbnailSize("LARGE").Do()
}

type driveWrapper struct {
	svc *drive.Service
}

// WrapDrive wraps a *drive.Service to satisfy DriveAPI.
func WrapDrive(svc *drive.Service) DriveAPI {
	return &driveWrapper{svc: svc}
}

func (w *driveWrapper) CopyFile(ctx context.Context, fileID string, file *drive.File) (*drive.File, error) {
	return w.svc.Files.Copy(fileID, file).SupportsAllDrives(true).Context(ctx).Do()
}
