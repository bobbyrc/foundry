// What each role may do in the browser. The server enforces the same rules; this only decides
// which buttons are drawn, so a viewer never sees a control that would be refused.
import type { Role } from '../types';

/** operate: act on work (approve, retry, cancel, pause, create, chat). administer: manage users. */
export type Action = 'operate' | 'administer';

export function can(role: Role | undefined, action: Action): boolean {
  if (role === undefined) return false;
  if (action === 'administer') return role === 'owner';
  return role === 'operator' || role === 'owner';
}
