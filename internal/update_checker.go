package internal

import (
	"bufio"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/regclient/regclient/types/ref"
)

type UpdateChecker struct {
	path     string
	registry *Registry
}

func NewUpdateChecker(path string, registry *Registry) *UpdateChecker {
	if registry == nil {
		registry = NewRegistry("")
	}
	return &UpdateChecker{path: path, registry: registry}
}

func (u *UpdateChecker) Check(major, minor, patch bool) ([]UpdateInfo, error) {
	updateInfos, err := u.createUpdateInfos()
	if err != nil {
		return nil, err
	}

	for i := range updateInfos {
		info := &updateInfos[i]

		version, err := semver.NewVersion(info.CurrentTag)
		if err != nil {
			// Not a semver tag. Comparing manifest digests is then the only way
			// to tell whether the image moved on, which covers both digest-pinned
			// images and repositories that publish commit tags instead of
			// versions (e.g. ghcr.io/vert-sh/vert).
			u.checkDigest(info)
			continue
		}

		tags, err := u.registry.FetchImageTags(info.ImageName)
		if err != nil {
			slog.Error("Skipping (failed fetching tags)", "image", info.ImageName, "path", info.FilePath)
			continue
		}

		latestVersion := FindLatestVersion(version, tags, major, minor, patch)
		if latestVersion == "" {
			continue
		}
		info.LatestTag = latestVersion

		// A reference that pins both a version and a digest (nginx:1.2.3@sha256:...)
		// must have its digest moved along with the tag, otherwise the rewritten
		// line would point the new tag at the old image.
		if info.CurrentDigest != "" {
			latestDigest, err := u.registry.FetchImageDigest(info.ImageName + ":" + latestVersion)
			if err != nil {
				slog.Error("Skipping (failed resolving digest for new tag)", "image", info.ImageName, "tag", latestVersion, "path", info.FilePath)
				info.LatestTag = ""
				continue
			}
			info.LatestDigest = latestDigest
		}
	}

	return updateInfos, nil
}

// checkDigest fills in the update fields for images whose tag is not a semantic
// version, by comparing the digest the reference currently resolves to against
// the digest of the repository's floating reference tag.
func (u *UpdateChecker) checkDigest(info *UpdateInfo) {
	// Whether the compose file itself pins a digest decides what gets rewritten
	// later: the digest in place, or the tag that now carries it.
	pinnedByDigest := info.CurrentDigest != ""

	// A bare floating tag records no digest in the compose file, so there is
	// nothing to compare it against — it already resolves to whatever is newest.
	if _, floating := mutableTags[info.CurrentTag]; floating && !pinnedByDigest {
		slog.Debug("Skipping (floating tag without digest)", "image", info.ImageName, "tag", info.CurrentTag, "path", info.FilePath)
		return
	}
	if info.CurrentTag == "" && !pinnedByDigest {
		slog.Warn("Skipping (no tag or digest)", "image", info.ImageName, "path", info.FilePath)
		return
	}

	latestDigest, err := u.registry.FetchImageDigest(info.ImageName + ":" + referenceTag)
	if err != nil {
		slog.Warn("Skipping (no "+referenceTag+" tag to compare against)", "image", info.ImageName, "path", info.FilePath)
		return
	}

	// Digest-pinned references carry their current digest in the file itself.
	currentDigest := info.CurrentDigest
	if !pinnedByDigest {
		currentDigest, err = u.registry.FetchImageDigest(info.ImageName + ":" + info.CurrentTag)
		if err != nil {
			slog.Warn("Skipping (failed resolving current digest)", "image", info.ImageName, "tag", info.CurrentTag, "path", info.FilePath)
			return
		}
		info.CurrentDigest = currentDigest
	}

	if currentDigest == latestDigest {
		return
	}
	info.LatestDigest = latestDigest

	// A pinned digest is rewritten in place and the tag, if any, stays as it is.
	if pinnedByDigest {
		return
	}

	// Otherwise the tag is all there is to rewrite, so the tag now carrying the
	// new digest has to be found.
	tags, err := u.registry.FetchImageTags(info.ImageName)
	if err != nil {
		slog.Error("Skipping (failed fetching tags)", "image", info.ImageName, "path", info.FilePath)
		info.LatestDigest = ""
		return
	}

	candidates, dropped := digestCandidates(tags, info.CurrentTag)
	if dropped > 0 {
		slog.Warn("Only probing a subset of tags", "image", info.ImageName, "probed", len(candidates), "skipped", dropped)
	}

	latestTag := findTagForDigest(u.registry, info.ImageName, candidates, latestDigest)
	if latestTag == "" {
		slog.Warn("Skipping (no tag matches the newest digest)", "image", info.ImageName, "tag", info.CurrentTag, "path", info.FilePath)
		info.LatestDigest = ""
		return
	}
	info.LatestTag = latestTag
}

func (u *UpdateChecker) createUpdateInfos() ([]UpdateInfo, error) {
	var updateInfos []UpdateInfo
	uniqueImages := make(map[string]struct{})

	file, err := os.Open(u.path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	imageNamePattern := regexp.MustCompile(`^\s*image:\s*(\S+)\s*$`)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		matches := imageNamePattern.FindStringSubmatch(line)
		if len(matches) > 1 {
			imageName := matches[1]
			name, tag, dgst := u.getNameTagAndDigest(imageName)
			imageKey := name + ":" + tag + "@" + dgst

			if _, exists := uniqueImages[imageKey]; !exists {
				uniqueImages[imageKey] = struct{}{}
				updateInfos = append(updateInfos, UpdateInfo{
					FilePath:      u.path,
					RawLine:       line,
					FullImageName: imageName,
					ImageName:     name,
					CurrentTag:    tag,
					CurrentDigest: dgst,
				})
			}
		}
	}

	return updateInfos, nil
}

func (u *UpdateChecker) naiveParsing(imageName string) (string, string) {
	parts := strings.Split(imageName, ":")
	if len(parts) < 2 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func (u *UpdateChecker) getNameTagAndDigest(imageName string) (string, string, string) {
	// Split off an explicit digest first, so the colon inside "@sha256:..."
	// cannot be mistaken for the separator introducing a tag.
	remainder := imageName
	dgst := ""
	if at := strings.LastIndex(imageName, "@"); at != -1 {
		remainder = imageName[:at]
		if candidate := imageName[at+1:]; IsDigest(candidate) {
			dgst = candidate
		}
	}

	// Only consider a tag if it is explicitly provided (i.e., after the last '/').
	lastSlash := strings.LastIndex(remainder, "/")
	lastColon := strings.LastIndex(remainder, ":")
	hasTag := lastColon > lastSlash

	rRef, err := ref.New(imageName)
	if err != nil {
		// Fallback to naive parsing if the reference can't be parsed
		name, tag := u.naiveParsing(remainder)
		return name, tag, dgst
	}

	name := rRef.Repository
	if rRef.Registry != "" && rRef.Registry != "docker.io" && rRef.Registry != "index.docker.io" {
		name = rRef.Registry + "/" + rRef.Repository
	}

	tag := ""
	if hasTag {
		tag = rRef.Tag
	}

	return name, tag, dgst
}
