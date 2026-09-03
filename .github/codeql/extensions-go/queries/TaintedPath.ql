/**
 * @name Uncontrolled data used in path expression
 * @description Accessing paths influenced by users can allow an attacker to access
 *              unexpected resources.
 * @kind path-problem
 * @problem.severity error
 * @security-severity 7.5
 * @precision high
 * @id go/path-injection
 * @tags security
 *       external/cwe/cwe-022
 *       external/cwe/cwe-023
 *       external/cwe/cwe-036
 *       external/cwe/cwe-073
 *       external/cwe/cwe-099
 */

// Same query as codeql/go-queries' Security/CWE-022/TaintedPath.ql, plus
// AegisPathSanitizers.qll's additional Sanitizer/SanitizerGuard classes,
// which register themselves against TaintedPath::Sanitizer /
// TaintedPath::SanitizerGuard purely by being imported here — see that
// file for what they cover and why. Kept at the same @id so GitHub code
// scanning treats this as the same rule: alerts that stop firing here are
// resolved, not orphaned under a new rule.
import go
import semmle.go.security.TaintedPath
import TaintedPath::Flow::PathGraph
import AegisPathSanitizers

from TaintedPath::Flow::PathNode source, TaintedPath::Flow::PathNode sink
where TaintedPath::Flow::flowPath(source, sink)
select sink.getNode(), source, sink, "This path depends on a $@.", source.getNode(),
  "user-provided value"
