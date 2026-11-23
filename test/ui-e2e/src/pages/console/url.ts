export function consoleUrl(path: string) {
  const base = process.env.CONSOLE_URL;
  if (!base) throw new Error('CONSOLE_URL is missing');
  return new URL(path, base).toString();
}
