// toolsHashFullCheck is a standalone tool that batch-fills SHA-256 hashes
// in xdev manifest files.
//
// It downloads each archive declared in a manifest (that currently has an
// all-zero placeholder hash), computes the real SHA-256, writes it back to
// the manifest YAML, and then cleans up the downloaded file — leaving only
// the computed hash.
//
// # Usage
//
//	cd toolsHashFullCheck
//	go run . -batch batch1           # process batch 1 (go, node, python, java)
//	go run . -batch batch1 -dry-run  # preview without downloading
//	go run .                          # process all batches
//	go run . -cleanup=false           # keep downloaded files for debugging
//
// # Progress Tracking
//
// The tool maintains a progress.json state file that records the status of
// every entry (pending → downloaded → hashed → cleaned). If the pipeline is
// interrupted, re-running the same batch will skip already-completed entries
// and resume from where it left off.
//
// # Batches
//
// Entries are grouped into 6 batches by tool category:
//
//	batch1: go, node, python, java              (~8 GB)
//	batch2: rust, ruby, php, perl, julia, lua   (~6 GB)
//	batch3: gradle, maven, cmake, ninja, make, git (~3 GB)
//	batch4: flutter, dart, deno, bun, kotlin, scala (~8 GB)
//	batch5: llvm, mingw, dotnet, haskell, erlang, zig (~10 GB)
//	batch6: android-* series                    (~5 GB)
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var (
		manifestsDir string
		cacheDir     string
		stateFile    string
		batch        string
		dryRun       bool
		cleanup      bool
		workers      int
		listBatches  bool
		finalize     bool
		resetFailed  bool
	)

	flag.StringVar(&manifestsDir, "manifests-dir", "./manifests", "path to manifests directory")
	flag.StringVar(&cacheDir, "cache-dir", "./downloads", "cache directory for downloaded archives")
	flag.StringVar(&stateFile, "state-file", "./progress.json", "path to progress state file")
	flag.StringVar(&batch, "batch", "", "batch to process (batch1-batch6, empty = all)")
	flag.BoolVar(&dryRun, "dry-run", false, "show what would be downloaded without modifying files")
	flag.BoolVar(&cleanup, "cleanup", true, "clean up downloaded files after hashing")
	flag.IntVar(&workers, "workers", 4, "number of concurrent download workers")
	flag.BoolVar(&listBatches, "list-batches", false, "list available batches and exit")
	flag.BoolVar(&finalize, "finalize", false, "write all hashed entries to manifests and clean up (use after interrupted runs)")
	flag.BoolVar(&resetFailed, "reset-failed", false, "reset all failed entries to pending (retry with fresh network)")

	flag.Parse()

	if listBatches {
		fmt.Println("Available batches:")
		for name, tools := range batches {
			fmt.Printf("  %s: %s\n", name, tools)
		}
		return
	}

	// Resolve relative paths against the working directory
	manifestsDir = resolvePath(manifestsDir)
	cacheDir = resolvePath(cacheDir)
	stateFile = resolvePath(stateFile)

	fmt.Printf("🔧 Configuration:\n")
	fmt.Printf("   Manifests: %s\n", manifestsDir)
	fmt.Printf("   Cache:     %s\n", cacheDir)
	fmt.Printf("   State:     %s\n", stateFile)
	fmt.Printf("   Batch:     %s\n", batchLabel(batch))
	fmt.Printf("   Dry run:   %v\n", dryRun)
	fmt.Printf("   Cleanup:   %v\n", cleanup)
	fmt.Printf("   Workers:   %d\n\n", workers)

	pipeline := NewPipeline(manifestsDir, cacheDir, stateFile, dryRun, cleanup, workers)
	pipeline.progress.SetPath(stateFile)

	if resetFailed {
		fmt.Print("🔄 Resetting all failed entries to pending...\n\n")
		pipeline.progress.ResetFailed()
		fmt.Println("Done. Run without -reset-failed to retry.")
		return
	}

	if finalize {
		fmt.Print("🔧 Finalize mode: writing hashed entries to manifests and cleaning up...\n\n")
		if err := pipeline.Finalize(); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Finalize failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := pipeline.Run(batch); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Pipeline failed: %v\n", err)
		os.Exit(1)
	}
}

// resolvePath converts a relative path to absolute, relative to the
// current working directory (not the executable's directory).
func resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// batchLabel returns a human-readable label for the batch selection.
func batchLabel(batch string) string {
	if batch == "" {
		return "all"
	}
	return batch
}
