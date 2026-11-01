// anx-access-reclaim rewrites a stopped workspace's state.sqlite offline to
// reclaim space through the verified compact-access cutover. It refuses to run
// against a live core; stop the service first.
package main
