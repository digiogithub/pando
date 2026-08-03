package models

import (
	"fmt"
	"sync"
	"testing"
)

// The catalogue is read from the TUI, the API layer and the agents while
// background refreshes register freshly discovered models. Before it became
// copy-on-write this combination crashed with "concurrent map iteration and map
// write". Run with -race to also catch unsynchronised access.
func TestSupportedModelsConcurrentReadWrite(t *testing.T) {
	const workers = 8
	const iterations = 200

	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(2)

		go func(worker int) {
			defer wg.Done()
			for i := range iterations {
				id := ModelID(fmt.Sprintf("__concurrency.w%d-%d", worker, i))
				RegisterDynamicModel(Model{ID: id, Name: "probe", Provider: ProviderMock})
				DeleteSupportedModels(id)
			}
		}(w)

		go func() {
			defer wg.Done()
			for range iterations {
				for _, model := range SupportedModels() {
					_ = model.ID
				}
			}
		}()
	}
	wg.Wait()
}

func TestSupportedModelsSnapshotIsStable(t *testing.T) {
	id := ModelID("__snapshot.probe")
	t.Cleanup(func() { DeleteSupportedModels(id) })

	before := SupportedModels()
	SetSupportedModel(Model{ID: id, Name: "probe", Provider: ProviderMock})

	if _, ok := before[id]; ok {
		t.Error("a snapshot taken before the write must not observe it")
	}
	if _, ok := SupportedModels()[id]; !ok {
		t.Error("a snapshot taken after the write must observe it")
	}
}

func TestSetSupportedModelsBatch(t *testing.T) {
	first := ModelID("__batch.one")
	second := ModelID("__batch.two")
	t.Cleanup(func() { DeleteSupportedModels(first, second) })

	SetSupportedModels(map[ModelID]Model{
		first:  {ID: first, Provider: ProviderMock},
		second: {ID: second, Provider: ProviderMock},
	})

	catalogue := SupportedModels()
	if _, ok := catalogue[first]; !ok {
		t.Error("first batch entry missing")
	}
	if _, ok := catalogue[second]; !ok {
		t.Error("second batch entry missing")
	}

	DeleteSupportedModels(first, second)
	catalogue = SupportedModels()
	if _, ok := catalogue[first]; ok {
		t.Error("first batch entry not deleted")
	}
	if _, ok := catalogue[second]; ok {
		t.Error("second batch entry not deleted")
	}
}
