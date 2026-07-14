export type OptionEntry = [key: string, value: string];

export function includeNonEmptyOptionEntries(
  requiredEntries: OptionEntry[],
  optionalEntries: OptionEntry[],
) {
  const normalizedOptionalEntries = optionalEntries.flatMap<OptionEntry>(
    ([key, value]) => {
      const normalizedValue = value.trim();
      return normalizedValue ? [[key, normalizedValue]] : [];
    },
  );

  return [...requiredEntries, ...normalizedOptionalEntries];
}
