package bridge

import (
	"errors"
	"fmt"
	"github.com/Jhorlin/agent-bridge/internal/diagnostics"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"time"
)

type Operation struct {
	Label  string    `json:"label"`
	File   string    `json:"file"`
	Before *Snapshot `json:"before"`
	After  *Snapshot `json:"after"`
}
type Journal struct {
	Version    int         `json:"version"`
	Operations []Operation `json:"operations"`
}
type Pending struct {
	Transaction string `json:"transaction"`
}
type Recovery struct {
	Status      string `json:"status"`
	Transaction string `json:"transaction,omitempty"`
}

type Options struct {
	// Observe receives only content-free diagnostic metadata; it must not panic.
	Observe diagnostics.Observer
	// BeforeWrite allows deterministic fault injection from isolated tests only.
	BeforeWrite func(int, Operation) error
	// ExpectedObservation makes watched application conditional on stable inputs.
	ExpectedObservation string
	// Resolutions requires an observation binding every explicit conflict choice.
	Resolutions map[string]string
	History     *HistoryChoice
}

var ErrObservationChanged = errors.New("inputs changed since the watched observation; retry planning")

func locked(c Config, fn func() error) (err error) {
	guarded := func() error {
		if err := checkCurrentConfig(c); err != nil {
			return err
		}
		return fn()
	}
	if err = validateLinks(c); err != nil {
		return err
	}
	if c.CoordinationDir != "" {
		return directoryLocked(c.CoordinationDir, func() error {
			if err := checkEnrollmentPending(c.CoordinationDir); err != nil {
				return err
			}
			if err := enforceOwnership(c); err != nil {
				return err
			}
			return directoryLocked(c.StateDir, guarded)
		})
	}
	return directoryLocked(c.StateDir, guarded)
}

// All coordinated profiles acquire the common lock before their state lock.
// Exclusive lock files fail closed after interruption; no PID-based lock stealing.
func directoryLocked(dir string, fn func() error) (err error) {
	if err = assertSafe(dir); err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock := filepath.Join(dir, "sync.lock")
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return ErrLockPresent
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close(), os.Remove(lock)) }()
	if _, err = fmt.Fprintf(f, `{"pid":%d,"started":%q}`, os.Getpid(), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return fn()
}
func allowedTarget(c Config, file string) bool {
	if file == manifestPath(c) {
		return true
	}
	for _, r := range c.Resources {
		for _, target := range r.CodexAgentExports {
			if target == file {
				return true
			}
		}
		for _, root := range r.Paths {
			if (r.Kind == "portable-file" || r.Kind == "mcp-config" || r.Kind == "instruction-file" || r.Kind == "agent-file" || r.Kind == "hook-config") && root == file {
				return true
			}
			if (r.Kind == "skill-directory" || r.Kind == "plugin-directory") && file != root && inside(root, file) {
				return true
			}
		}
	}
	return false
}
func rollbackWithOwnership(c Config, j Journal, receipt *syncOwnership) error {
	if err := validateSyncOwnership(j, receipt); err != nil {
		return err
	}
	seen := map[string]bool{}
	for index, op := range j.Operations {
		if !filepath.IsAbs(op.File) || filepath.Clean(op.File) != op.File || !allowedTarget(c, op.File) || seen[op.File] {
			return fmt.Errorf("recovery journal contains an invalid target")
		}
		seen[op.File] = true
		if err := valid(op.Before); err != nil {
			return err
		}
		if op.After == nil {
			return fmt.Errorf("invalid after snapshot")
		}
		if err := valid(op.After); err != nil {
			return err
		}
		current, err := snapshot(op.File)
		if err != nil {
			return err
		}
		if err := checkSyncWriteOwnership(receipt, index, op, current); err != nil {
			return err
		}
		if !equal(current, op.Before) && !equal(current, op.After) {
			return fmt.Errorf("recovery blocked by a later edit: %s", op.Label)
		}
	}
	for i := len(j.Operations) - 1; i >= 0; i-- {
		op := j.Operations[i]
		current, err := snapshot(op.File)
		if err != nil {
			return err
		}
		if err := checkSyncWriteOwnership(receipt, i, op, current); err != nil {
			return err
		}
		if equal(current, op.Before) {
			continue
		}
		if !equal(current, op.After) {
			return fmt.Errorf("recovery blocked by a later edit: %s", op.Label)
		}
		if op.Before == nil {
			err = os.Remove(op.File)
		} else {
			err = writeSnapshot(op.File, op.Before)
		}
		if err != nil {
			return err
		}
	}
	return os.Remove(pendingPath(c))
}

var transactionPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func Recover(c Config) (Recovery, error) {
	return RecoverObserved(c, nil)
}

func RecoverObserved(c Config, sink diagnostics.Observer) (recovered Recovery, failure error) {
	transaction := ""
	defer func() { observe(sink, "recover", "bridge.recovery", "", "", transaction, failure) }()
	result := Recovery{Status: "nothing-to-recover"}
	err := locked(c, func() error {
		if err := checkFileChangePending(c); err != nil {
			return err
		}
		pending, err := snapshot(pendingPath(c))
		if err != nil || pending == nil {
			return err
		}
		var pointer syncPending
		if err = decodeEnrollmentJSON(pending, &pointer); err != nil {
			return err
		}
		if !transactionPattern.MatchString(pointer.Transaction) {
			return fmt.Errorf("invalid recovery transaction")
		}
		transaction = pointer.Transaction
		stored, err := snapshot(filepath.Join(c.StateDir, "backups", pointer.Transaction, "journal.json"))
		if err != nil {
			return err
		}
		if stored == nil {
			return fmt.Errorf("recovery journal is missing; manual inspection required")
		}
		var j Journal
		if err = decodeEnrollmentJSON(stored, &j); err != nil {
			return err
		}
		if j.Version != 1 || j.Operations == nil {
			return fmt.Errorf("invalid recovery journal")
		}
		receipt, err := readSyncOwnership(c, pointer, j)
		if err != nil {
			return err
		}
		if err = rollbackWithOwnership(c, j, receipt); err != nil {
			return err
		}
		result = Recovery{Status: "recovered", Transaction: pointer.Transaction}
		return nil
	})
	return result, err
}
func Apply(c Config, options Options) (output []Summary, failure error) {
	stage, resource, path, tx := "lock", "", "", ""
	defer func() {
		if failure != nil {
			observe(options.Observe, stage, "bridge.transaction", resource, path, tx, failure)
		}
	}()
	var summaries []Summary
	err := locked(c, func() error {
		stage = "plan"
		result, err := PlanObserved(c, options.Observe)
		if err != nil {
			return err
		}
		observation := Observation(c, result)
		if options.History != nil {
			if options.Resolutions != nil || !digestPattern.MatchString(options.ExpectedObservation) {
				return fmt.Errorf("historical restore requires its own reviewed observation")
			}
			observation, err = prepareHistory(c, &result, *options.History)
			if err != nil {
				return err
			}
		}
		if options.Resolutions != nil {
			if !digestPattern.MatchString(options.ExpectedObservation) {
				return fmt.Errorf("conflict resolution requires a reviewed observation")
			}
			observation = resolutionObservation(c, result, options.Resolutions)
		}
		if options.ExpectedObservation != "" && observation != options.ExpectedObservation {
			return ErrObservationChanged
		}
		if options.Resolutions != nil {
			if err := resolvePlan(&result, options.Resolutions); err != nil {
				return err
			}
		}
		if result.HasConflicts() {
			return ErrConflicts
		}
		operations := []Operation{}
		for _, item := range result.Items {
			stage, resource = "prepare", item.Resource.ID
			for _, side := range sides {
				path = item.Paths[side]
				current, err := readItemSide(item, side)
				if err != nil {
					return err
				}
				if !equal(current, item.Values[side]) {
					return fmt.Errorf("input changed during planning: %s", item.Key)
				}
			}
			for _, side := range item.Writes {
				stage, path = "render", item.Paths[side]
				before := item.Values[side]
				content := item.Content
				switch item.Adapter {
				case "plugin-agent":
					content, err = renderPluginAgent(item, side, item.Content, before)
				case "skill-invocation":
					content, err = renderSkillResource(item.Resource, side, item.Content, before)
				case "instruction-file":
					content, err = renderInstructions(side, item.Content, before)
				case "agent-file":
					content, err = renderAgentResource(item.Resource, side, item.Content, before)
				case "hook-config":
					content, err = renderHooks(side, item.Content, before)
				case "mcp":
					content, err = renderMCP(item.Resource, side, item.Content, before)
				case "plugin-mcp":
					content, err = renderPluginMCP(item.Resource, side, item.Content, before)
				case "plugin-manifest":
					content, err = renderPluginManifest(item.Resource, side, item.Content)
				}
				if err != nil {
					return err
				}
				var roundTrip *Snapshot
				stage = "validate"
				switch item.Adapter {
				case "plugin-agent":
					roundTrip, err = normalizePluginAgent(item, side, content)
				case "skill-invocation":
					roundTrip, err = normalizeSkillResource(item.Resource, side, content)
				case "plugin-command":
					roundTrip, err = normalizePluginCommand(content)
				case "skill-metadata":
					roundTrip, err = normalizeSkill(content)
				case "instruction-file":
					roundTrip, err = normalizeInstructions(side, content)
				case "agent-file":
					roundTrip, err = normalizeAgentResource(item.Resource, side, content)
				case "hook-config":
					roundTrip, err = normalizeHooks(side, content)
				case "mcp":
					roundTrip, err = normalizeMCP(item.Resource, side, content)
				case "plugin-mcp":
					roundTrip, err = normalizePluginMCP(item.Resource, side, content)
				case "plugin-manifest":
					roundTrip, err = normalizePluginManifest(content)
				default:
					roundTrip = content
				}
				if err != nil {
					return err
				}
				if fingerprint(roundTrip) != item.Digest {
					return fmt.Errorf("adapter round-trip validation failed for %s", item.ID)
				}
				if item.Adapter == "skill-invocation" && side == "codex" {
					extra, err := skillBundleOperations(item, before, content)
					if err != nil {
						return err
					}
					operations = append(operations, extra...)
					continue
				}
				mode := uint32(0600)
				if before != nil {
					mode = before.Mode
				}
				mode = (mode & 0666) | (content.Mode & 0111)
				operations = append(operations, Operation{item.Key + "-" + side, item.Paths[side], before, &Snapshot{content.Data, mode}})
			}
			result.Manifest.Files[item.Key] = item.Digest
			if item.Adapter == "mcp" || (item.Adapter == "plugin-mcp" && featureResourceID(item.ID)) {
				resource := item.Resource
				if item.Adapter == "plugin-mcp" {
					resource.ID = item.ID + "@mcp"
				}
				if err := recordMCPBaselines(resource, item.Content, &result.Manifest); err != nil {
					return err
				}
			}
			if item.Adapter == "skill-invocation" && featureResourceID(item.ID) {
				if err := recordConventionSkillPolicy(item, &result.Manifest); err != nil {
					return err
				}
			}
		}
		for _, r := range c.Resources {
			result.Manifest.Resources[r.ID] = r
		}
		var original Manifest
		if result.ManifestBefore == nil || decode(result.ManifestBefore, &original) != nil || !reflect.DeepEqual(original, result.Manifest) {
			after, err := encoded(result.Manifest)
			if err != nil {
				return err
			}
			operations = append(operations, Operation{"manifest", manifestPath(c), result.ManifestBefore, after})
		}
		summaries = result.Summaries()
		if len(operations) == 0 {
			return nil
		}
		transaction, err := uuid()
		if err != nil {
			return err
		}
		tx, stage, resource, path = transaction, "journal", "", ""
		observe(options.Observe, stage, "bridge.transaction", "", "", tx, nil)
		journal := Journal{1, operations}
		if err = writeJSON(filepath.Join(c.StateDir, "backups", transaction, "journal.json"), journal); err != nil {
			return err
		}
		receipt, err := newSyncOwnership(journal)
		if err != nil {
			return err
		}
		if err = writeJSON(syncOwnershipPath(c, transaction), receipt); err != nil {
			return err
		}
		if err = writeJSON(pendingPath(c), syncPending{Transaction: transaction, CreationOwnership: true, WriteOwnership: true}); err != nil {
			return err
		}
		writeErr := func() error {
			for index, op := range operations {
				stage, path, resource = "write", op.File, ""
				for _, r := range c.Resources {
					for _, root := range r.Paths {
						if inside(root, op.File) {
							resource = r.ID
						}
					}
					for _, export := range r.CodexAgentExports {
						if export == op.File {
							resource = r.ID
						}
					}
				}
				observe(options.Observe, stage, "bridge.transaction", resource, path, tx, nil)
				if options.BeforeWrite != nil {
					if err := options.BeforeWrite(index, op); err != nil {
						return err
					}
				}
				if err := validateLinks(c); err != nil {
					return err
				}
				current, err := snapshot(op.File)
				if err != nil {
					return err
				}
				if !equal(current, op.Before) {
					return fmt.Errorf("input changed before write: %s", op.Label)
				}
				if op.Before == nil {
					// A fully written temporary inode is linked exclusively into place.
					// Never replace an external creator that wins after our precheck.
					if err = os.MkdirAll(filepath.Dir(op.File), 0700); err != nil {
						return err
					}
					err = createRosterExclusive(op.File, op.After)
					if err == nil {
						receipt.Created[index] = true
					}
				} else {
					err = writeSnapshot(op.File, op.After)
				}
				if err != nil {
					return err
				}
				receipt.Written[index] = true
				stage = "receipt"
				if err = writeJSON(syncOwnershipPath(c, transaction), receipt); err != nil {
					return err
				}
			}
			stage, resource, path = "commit", "", ""
			return os.Remove(pendingPath(c))
		}()
		if writeErr != nil {
			observe(options.Observe, stage, "bridge.transaction", resource, path, tx, writeErr)
			stage = "rollback"
			if recoveryErr := rollbackWithOwnership(c, journal, receipt); recoveryErr != nil {
				observe(options.Observe, stage, "bridge.transaction", "", "", tx, recoveryErr)
				return fmt.Errorf("%w; %v. Pending transaction retained; run recover after inspection", writeErr, recoveryErr)
			}
			observe(options.Observe, stage, "bridge.transaction", "", "", tx, nil)
			// The original write failure has already been recorded.
			stage = "operation"
			return fmt.Errorf("%w; transaction rolled back", writeErr)
		}
		observe(options.Observe, "commit", "bridge.transaction", "", "", tx, nil)
		return nil
	})
	return summaries, err
}
