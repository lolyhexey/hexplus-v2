import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

// cn: standard shadcn class-name helper. Combines clsx (conditional
// class objects) with tailwind-merge (dedupes conflicting Tailwind
// utilities so the caller can override defaults cleanly).
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
