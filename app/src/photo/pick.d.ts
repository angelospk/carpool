// The contract both pickers satisfy.
//
// pick.web.ts and pick.native.ts implement it; Metro picks one by platform and
// ignores this file, while TypeScript reads this one and so type-checks the
// screen against the shape rather than against whichever platform it resolved.

/**
 * Opens the platform's picker and returns the chosen image, already reduced to
 * the size the server keeps, or null if the person backed out.
 *
 * The result is whatever the fetch body accepts: a Blob on the web, and on a
 * device an expo-file-system File, which implements Blob over a local path.
 */
export declare function pickPhoto(): Promise<Blob | null>;
