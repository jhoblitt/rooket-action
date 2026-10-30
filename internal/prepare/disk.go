package prepare

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/jhoblitt/rooket-action/internal/ghcmd"
)

const (
	gibBytes     = uint64(1) << 30
	baseGiB      = 30
	perWorkerGiB = 10

	// root is the filesystem rooket's kind nodes and images land on.
	root = "/"
)

// toolchains are what rgw-go's and rooket's workflows delete to make room,
// and nothing more: the rest of the tool cache stays, Go above all, because
// a consumer's later steps build with the Go that actions/setup-go put there.
var toolchains = []string{
	"/usr/share/dotnet",
	"/usr/local/lib/android",
	"/opt/ghc",
	"/opt/hostedtoolcache/CodeQL",
	"/usr/local/.ghcup",
	"/usr/lib/jvm",
	"/usr/share/swift",
	"/usr/local/share/powershell",
}

// Threshold returns the bytes EnsureDisk needs free: minFreeGiB GiB or, when
// minFreeGiB is below 1, 30 GiB plus 10 GiB per worker. workers below 1
// counts as 1.
func Threshold(workers, minFreeGiB int) uint64 {
	if minFreeGiB > 0 {
		return uint64(minFreeGiB) * gibBytes
	}
	return (baseGiB + perWorkerGiB*uint64(max(workers, 1))) * gibBytes
}

// EnsureDisk makes sure need bytes are free on /, as free measures it. When
// they are not, it deletes the preinstalled toolchains with one sudo rm -rf
// through r, which takes minutes, and measures again.
//
// need is a coarse estimate, so nothing in the disk step fails the run. A
// failed measurement, a failed deletion, and still being short afterwards
// each write a ::warning:: workflow command to warn, and EnsureDisk goes on
// or returns nil; a first measurement that fails deletes nothing, because
// deleting blind is not worth minutes. The one error is ctx's: once ctx is
// done, EnsureDisk returns it in place of a warning, so that a canceled job
// does not report success.
func EnsureDisk(ctx context.Context, r Runner, free func(path string) (uint64, error), need uint64, warn io.Writer) error {
	got, err := free(root)
	if err != nil {
		return warning(ctx, warn, fmt.Sprintf("measuring free disk space on %s: %v; deleting nothing", root, err))
	}
	if got >= need {
		return nil
	}
	slog.InfoContext(ctx, "deleting preinstalled toolchains to free disk space",
		slog.Float64("free_gib", gib(got)),
		slog.Float64("need_gib", gib(need)))
	if err = r.Run(ctx, nil, "sudo", append([]string{"rm", "-rf"}, toolchains...)...); err != nil {
		if werr := warning(ctx, warn, fmt.Sprintf("deleting preinstalled toolchains: %v", err)); werr != nil {
			return werr
		}
	}
	if got, err = free(root); err != nil {
		return warning(ctx, warn, fmt.Sprintf("measuring free disk space on %s after deleting preinstalled toolchains: %v", root, err))
	}
	if got < need {
		return warning(ctx, warn, fmt.Sprintf("%.1f GiB free on %s after deleting preinstalled toolchains; %.1f GiB needed",
			gib(got), root, gib(need)))
	}
	return nil
}

// warning writes msg to w as a ::warning:: workflow command and returns nil
// or, once ctx is done, writes nothing and returns ctx's error.
func warning(ctx context.Context, w io.Writer, msg string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("freeing disk space: %w", err)
	}
	ghcmd.Warning(w, msg)
	return nil
}

func gib(b uint64) float64 {
	return float64(b) / float64(gibBytes)
}
