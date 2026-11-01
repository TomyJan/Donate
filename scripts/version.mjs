const versionPattern = /^\d+\.\d+\.\d+$/;
const tagPattern = /^v?\d+\.\d+\.\d+$/;
export function metadata({
  commit,
  exactTag = '',
  nearestTag = '',
  version = '',
  release = false,
  dirty = false,
}) {
  if (!/^[0-9a-f]{40,64}$/.test(commit)) throw new Error('Invalid Git commit');
  if (version && !versionPattern.test(version)) throw new Error('Invalid version');
  const tag = tagPattern.test(exactTag) ? exactTag : '';
  const result =
    version || (tag || (tagPattern.test(nearestTag) ? nearestTag : '') || '0.0.1').replace(/^v/, '');
  if (release && (dirty || !tag || tag.replace(/^v/, '') !== result))
    throw new Error('Release requires a clean checkout at the matching version tag');
  return { version: result, tag, commit };
}
