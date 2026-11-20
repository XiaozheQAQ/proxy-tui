package ipc

// Transport is selected per platform: Windows uses go-winio Named Pipes with a
// user-SID DACL; Unix uses a 0600 Unix-domain socket with peer UID checks.
