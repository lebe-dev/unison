/** remainingSeconds returns the full seconds left until the deadline. */
export function remainingSeconds(expiresAt: number, now: number): number {
  return Math.max(0, Math.ceil((expiresAt - now) / 1000));
}

/** formatRemaining renders seconds as minutes and seconds, for example 2:05. */
export function formatRemaining(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds));
  const minutes = Math.floor(value / 60);
  const rest = String(value % 60).padStart(2, "0");
  return `${minutes}:${rest}`;
}

/** groupDigits splits six digits into two groups for reading. */
export function groupDigits(digits: string): string {
  return digits.replace(/\s+/g, "").replace(/(\d{3})(?=\d)/g, "$1 ");
}

/** sasEmoji returns the four emoji of a short authentication string. */
export function sasEmoji(emoji: readonly string[]): string[] {
  return emoji.slice(0, 4);
}
