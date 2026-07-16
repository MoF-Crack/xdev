export function sameEnvIdentity(left, right) {
  return !!left && !!right
    && (left.toolName ?? left.tool ?? '') === (right.toolName ?? right.tool ?? '')
    && (left.level ?? 'user') === (right.level ?? 'user')
    && left.name === right.name;
}
