package cronjob

import (
	"context"

	mesnadamodels "github.com/digiogithub/pando/pkg/mesnada/models"
)

type OrchestratorClient interface {
	Spawn(ctx context.Context, req mesnadamodels.SpawnRequest) (*mesnadamodels.Task, error)
	ListTasks(req mesnadamodels.ListRequest) ([]*mesnadamodels.Task, error)
}

// CronJobFiredPayload is published via pubsub whenever a scheduled or manual
// cronjob dispatch succeeds and a Mesnada task has been created.
type CronJobFiredPayload struct {
	JobName string
	TaskID  string
}
