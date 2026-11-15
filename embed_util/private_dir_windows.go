package embed_util

// ensurePrivateDir is a no-op on Windows, where the user cache dir is inside the user's profile.
func ensurePrivateDir(string) error {
	return nil
}
