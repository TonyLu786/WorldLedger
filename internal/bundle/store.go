package bundle

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/worldledger/worldledger-mc/internal/archive"
	"github.com/worldledger/worldledger-mc/internal/cas"
	"github.com/worldledger/worldledger-mc/internal/model"
)

// Storing a bundle's components.
//
// A full-height chunk arrives as about fifty of them, and each one is opened,
// read, hashed, written to a temporary, forced to disk and renamed. Measured at
// 4.24 ms apiece, which is 212 ms for one observation and almost all of it
// spent waiting on the disk rather than on a processor.
//
// They do not depend on each other. Each is addressed by its own digest, the
// object store deliberately takes no archive lock, and two writes of the same
// digest already resolve to one object because the store checks before it
// commits. So they are stored several at a time.
//
// It is worth less than it looks. Measured over twelve bundles of fifty
// components on this machine, four at a time took 276 ms per bundle down to
// 189: a third, not the fourfold the arithmetic of four workers suggests. The
// waiting that overlaps is the reading and the hashing; the rename and the
// forcing to disk are one volume's metadata and they queue there however many
// callers ask at once. Getting the rest would mean one durability barrier per
// bundle instead of fifty, which is a different change with a different
// argument about what survives a power cut.
//
// What stays sequential is the deciding. Which file a component resolves to,
// whether it escapes the bundle, whether it is the manifest wearing another
// name: none of that is faster in parallel and all of it is the part that must
// not be got wrong. It runs exactly as it did, in order, and only the waiting
// is shared out.

// importWorkers is how many components are stored at once.
//
// Small on purpose. The gain is in overlapping waits on one disk, which stops
// paying almost immediately, and a bundle is fifty files rather than fifty
// thousand. A number larger than this trades a queue on the disk for a queue in
// the program.
func importWorkers() int {
	if forcedImportWorkers > 0 {
		return forcedImportWorkers
	}
	const most = 4
	if cpus := runtime.NumCPU(); cpus < most {
		return cpus
	}
	return most
}

// forcedImportWorkers exists so a test can drive the sequential path, which is
// the one a single-processor machine takes and which nothing would otherwise
// reach. Zero everywhere else.
var forcedImportWorkers int

type componentResult struct {
	name string
	ref  model.BlobRef
	err  error
}

// storeComponents puts every component of a bundle into the object store.
//
// Errors are reported in the bundle's own order rather than in whichever order
// the workers happened to fail, so the same broken bundle always produces the
// same message. A bundle with two problems has one of them, and which one does
// not depend on how a machine was scheduled that day.
func storeComponents(a archive.Archive, prepared preparedBundle) (map[string]model.BlobRef, error) {
	results := make([]componentResult, len(prepared.components))

	workers := importWorkers()
	if workers < 2 || len(prepared.components) < 2 {
		resolver := pathResolver{}
		for index, component := range prepared.components {
			results[index] = storeOne(a, prepared, component, resolver)
		}
	} else {
		next := make(chan int)
		var wait sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				// One resolver per worker. It is a cache and nothing more, so a
				// worker with its own misses a little and shares nothing; the
				// map it replaces is written to on every miss, which is a race
				// the moment two of these run at once.
				resolver := pathResolver{}
				for index := range next {
					results[index] = storeOne(a, prepared, prepared.components[index], resolver)
				}
			}()
		}
		for index := range prepared.components {
			next <- index
		}
		close(next)
		wait.Wait()
	}

	refs := make(map[string]model.BlobRef, len(results))
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		refs[result.name] = result.ref
	}
	return refs, nil
}

// storeOne is the body of the loop this replaced, unchanged in what it checks
// and in what order.
func storeOne(a archive.Archive, prepared preparedBundle, component preparedComponent,
	resolver pathResolver) componentResult {

	f, info, err := openRegularWithin(prepared.root, prepared.realRoot, component.descriptor.Path, resolver)
	if err != nil {
		return componentResult{name: component.name,
			err: invalidf("component %q: %v", component.name, err)}
	}
	if os.SameFile(prepared.manifestInfo, info) {
		_ = f.Close()
		return componentResult{name: component.name,
			err: invalidf("component %q resolves to bundle.json", component.name)}
	}

	expected := model.BlobRef{
		Algorithm: component.descriptor.Algorithm,
		Digest:    component.descriptor.Digest,
		Size:      component.descriptor.Size,
	}
	ref, putErr := a.CAS.PutVerified(f, expected)
	closeErr := f.Close()
	if putErr != nil {
		if errors.Is(putErr, cas.ErrObjectMismatch) {
			return componentResult{name: component.name,
				err: invalidf("component %q changed while importing: %v", component.name, putErr)}
		}
		return componentResult{name: component.name,
			err: fmt.Errorf("store component %q: %w", component.name, putErr)}
	}
	if closeErr != nil {
		return componentResult{name: component.name,
			err: fmt.Errorf("close component %q: %w", component.name, closeErr)}
	}
	return componentResult{name: component.name, ref: ref}
}
