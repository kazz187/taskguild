/**
 * Returns a new array with `item` toggled: added if absent, removed if present.
 */
export function toggleArrayItem<T>(array: T[], item: T): T[] {
  return array.includes(item)
    ? array.filter(i => i !== item)
    : [...array, item]
}
