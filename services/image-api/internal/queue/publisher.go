package queue

import "context"

// ServicePublisher adapts VariantQueue to the image service publisher interface.
type ServicePublisher struct {
	Queue *VariantQueue
}

func (p ServicePublisher) EnqueueVariantJob(ctx context.Context, imageID, storagePath string, userID *string) error {
	job := VariantJob{
		ImageID:     imageID,
		StoragePath: storagePath,
	}
	if userID != nil {
		job.UserID = *userID
	}
	return p.Queue.Enqueue(ctx, job)
}
