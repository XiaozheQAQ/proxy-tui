package ipc

// Transport is selected per platform: Windows uses go-winio Named Pipes with a
// Logon-SID DACL; Unix uses a 0600 Unix-domain socket.
