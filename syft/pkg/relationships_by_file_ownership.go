package pkg

import (
	"github.com/anchore/syft/internal/log"
	"github.com/anchore/syft/syft/artifact"
	"github.com/bmatcuk/doublestar/v2"
	"github.com/scylladb/go-set/strset"
)

var globsForbiddenFromBeingOwned = []string{
	// any OS DBs should automatically be ignored to prevent cyclic issues (e.g. the "rpm" RPM owns the path to the
	// RPM DB, so if not ignored that package would own all other packages on the system).
	ApkDBGlob,
	DpkgDBGlob,
	RpmDBGlob,
	// DEB packages share common copyright info between, this does not mean that sharing these paths implies ownership.
	"/usr/share/doc/**/copyright",
}

type ownershipByFilesMetadata struct {
	Files []string `json:"files"`
}

// RelationshipsByFileOwnership creates a package-to-package relationship based on discovering which packages have
// evidence locations that overlap with ownership claim from another package's package manager metadata.
func RelationshipsByFileOwnership(catalog *Catalog) []artifact.Relationship {
	var relationships = findOwnershipByFilesRelationships(catalog)

	var edges []artifact.Relationship
	for parent, children := range relationships {
		for child, files := range children {
			edges = append(edges, artifact.Relationship{
				From: catalog.byID[parent],
				To:   catalog.byID[child],
				Type: artifact.OwnershipByFileOverlapRelationship,
				Data: ownershipByFilesMetadata{
					Files: files.List(),
				},
			})
		}
	}

	return edges
}

// findOwnershipByFilesRelationships find overlaps in file ownership with a file that defines another package. Specifically, a .Location.Path of
// a package is found to be owned by another (from the owner's .Metadata.Files[]).
func findOwnershipByFilesRelationships(catalog *Catalog) map[artifact.ID]map[artifact.ID]*strset.Set {
	var relationships = make(map[artifact.ID]map[artifact.ID]*strset.Set)

	if catalog == nil {
		return relationships
	}

	for _, candidateOwnerPkg := range catalog.Sorted() {
		id := candidateOwnerPkg.ID()
		if candidateOwnerPkg.Metadata == nil {
			continue
		}

		// check to see if this is a file owner
		pkgFileOwner, ok := candidateOwnerPkg.Metadata.(FileOwner)
		if !ok {
			continue
		}
		for _, ownedFilePath := range pkgFileOwner.OwnedFiles() {
			if matchesAny(ownedFilePath, globsForbiddenFromBeingOwned) {
				// we skip over known exceptions to file ownership, such as the RPM package owning
				// the RPM DB path, otherwise the RPM package would "own" all RPMs, which is not intended
				continue
			}

			// look for package(s) in the catalog that may be owned by this package and mark the relationship
			for _, subPackage := range catalog.PackagesByPath(ownedFilePath) {
				subID := subPackage.ID()
				if subID == id {
					continue
				}
				if _, exists := relationships[id]; !exists {
					relationships[id] = make(map[artifact.ID]*strset.Set)
				}

				if _, exists := relationships[id][subID]; !exists {
					relationships[id][subID] = strset.New()
				}
				relationships[id][subID].Add(ownedFilePath)
			}
		}
	}

	return relationships
}

func matchesAny(s string, globs []string) bool {
	for _, g := range globs {
		matches, err := doublestar.Match(g, s)
		if err != nil {
			log.Errorf("failed to match glob=%q : %+v", g, err)
		}
		if matches {
			return true
		}
	}
	return false
}
