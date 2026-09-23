// Where to go once signing in is done, when somebody started from a route link.
//
// Without it the auth gate sends everybody to the home tab, and the route that
// brought them to the app is gone. Only a route link is kept: this is not a
// general redirect, and an arbitrary path handed across sign-in is one more
// thing that could be abused.
//
// In memory, because the whole flow — link, phone, code, profile — happens in
// one session of the app.

let pending: string | null = null;

const ROUTE_LINK = /^\/r\/[A-Za-z0-9_-]{16,64}$/;

export function rememberReturn(path: string): void {
  pending = ROUTE_LINK.test(path) ? path : null;
}

/** The remembered path, once. */
export function takeReturn(): string | null {
  const out = pending;
  pending = null;
  return out;
}
