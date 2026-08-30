package registry

import (
	"log/slog"
	"sync"
)

// probeWorkers limits how many tags are resolved at once. The probes are
// latency-bound, so this sits well above the core count.
const probeWorkers = 24

// TagForDigest probes candidate tags of image until one resolves to digest,
// returning "" when none matches. Probes run concurrently and stop early.
func TagForDigest(f Fetcher, image string, candidates []string, digest string) string {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		match string
		queue = make(chan string)
	)

	found := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return match != ""
	}

	for range min(probeWorkers, len(candidates)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tag := range queue {
				if found() {
					continue
				}

				candidateDigest, err := f.Digest(image + ":" + tag)
				if err != nil {
					slog.Debug("Failed probing tag", "image", image, "tag", tag, "error", err)
					continue
				}
				if candidateDigest != digest {
					continue
				}

				mu.Lock()
				if match == "" {
					match = tag
				}
				mu.Unlock()
			}
		}()
	}

	for _, tag := range candidates {
		if found() {
			break
		}
		queue <- tag
	}
	close(queue)
	wg.Wait()

	return match
}
