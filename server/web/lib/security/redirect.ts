const localOrigin = 'https://poolnext.invalid';

export function getSafeInternalRedirect(
  value: string | null | undefined,
  fallback = '/',
) {
  if (!value) {
    return fallback;
  }

  const candidate = value.trim();
  if (
    !candidate.startsWith('/') ||
    candidate.startsWith('//') ||
    candidate.includes('\\') ||
    /[\u0000-\u001f\u007f]/u.test(candidate)
  ) {
    return fallback;
  }

  try {
    const parsed = new URL(candidate, localOrigin);
    const decodedPath = decodeURIComponent(parsed.pathname);
    if (
      parsed.origin !== localOrigin ||
      decodedPath.startsWith('//') ||
      decodedPath.includes('\\') ||
      /[\u0000-\u001f\u007f]/u.test(decodedPath)
    ) {
      return fallback;
    }
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return fallback;
  }
}
