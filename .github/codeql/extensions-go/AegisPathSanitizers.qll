/**
 * Teaches the standard `go/path-injection` (CWE-022) query about Aegis's own
 * path-confinement helpers, which CodeQL's generic taint tracking cannot see
 * through on its own: `sandbox.ValidatePath`/`ValidatePathIn` resolve
 * symlinks and reject any result outside the given root(s) before returning
 * it, `resolveRead`/`resolveWrite` (internal/tool/builtin) wrap that check
 * for every filesystem tool, `resolveSafeImagePath` (internal/server) does
 * the same for the image-serving endpoint, and `(*Server).workdirAllowed`
 * gates a workdir against the configured trust boundary before any
 * filesystem call runs against it.
 *
 * Without this file, every one of the ~50 call sites downstream of these
 * helpers is flagged as if the path reaching the filesystem sink were still
 * raw, untrusted, tool-call input — see the repository's CodeQL triage notes
 * for the alerts this closes.
 */

import go
import semmle.go.security.TaintedPath

/**
 * The validated path returned by `sandbox.ValidatePath` or
 * `sandbox.ValidatePathIn`, considered confined to the given root(s) and
 * therefore safe against path-traversal.
 */
class AegisSandboxValidatedPath extends TaintedPath::Sanitizer {
  AegisSandboxValidatedPath() {
    exists(Function f, FunctionOutput outp |
      f.hasQualifiedName("github.com/fiddler110/aegis/internal/sandbox",
        ["ValidatePath", "ValidatePathIn"]) and
      outp.isResult(0) and
      this = outp.getNode(f.getACall())
    )
  }
}

/**
 * The validated absolute path returned by `resolveRead`/`resolveWrite`
 * (internal/tool/builtin), which resolve a tool's `path` argument through
 * `sandbox.ValidatePathIn` before any filesystem call is allowed to use it.
 */
class AegisBuiltinResolvedPath extends TaintedPath::Sanitizer {
  AegisBuiltinResolvedPath() {
    exists(Function f, FunctionOutput outp |
      f.hasQualifiedName("github.com/fiddler110/aegis/internal/tool/builtin",
        ["resolveRead", "resolveWrite"]) and
      outp.isResult(0) and
      this = outp.getNode(f.getACall())
    )
  }
}

/**
 * The validated path returned by `resolveSafeImagePath` (internal/server),
 * which constrains an API-supplied image path to the working-directory tree
 * before the image-serving handler opens it.
 */
class AegisImagePathSanitizer extends TaintedPath::Sanitizer {
  AegisImagePathSanitizer() {
    exists(Function f, FunctionOutput outp |
      f.hasQualifiedName("github.com/fiddler110/aegis/internal/server", "resolveSafeImagePath") and
      outp.isResult(0) and
      this = outp.getNode(f.getACall())
    )
  }
}

/**
 * A workdir guarded by `(*Server).workdirAllowed`, considered a sanitizer
 * guard for path traversal: the boolean result gates every filesystem call
 * that follows against the session's configured trust boundary.
 */
class AegisWorkdirAllowedGuard extends TaintedPath::SanitizerGuard, DataFlow::CallNode {
  AegisWorkdirAllowedGuard() {
    exists(Method m, FunctionInput inp |
      m.hasQualifiedName("github.com/fiddler110/aegis/internal/server", "Server", "workdirAllowed") and
      inp.isParameter(0) and
      this = m.getACall() and
      exists(inp.getNode(this))
    )
  }

  override predicate checks(Expr e, boolean branch) {
    exists(FunctionInput inp, Method m |
      m.hasQualifiedName("github.com/fiddler110/aegis/internal/server", "Server", "workdirAllowed") and
      inp.isParameter(0)
    |
      e = inp.getNode(this).asExpr() and branch = true
    )
  }
}
