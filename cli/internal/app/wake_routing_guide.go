package app

func wakeRoutingGuideText() string {
	return `Wake routing

The core router turns @<name>.<host> mentions into durable wakes. A derived
agent is taggable while its host is active and its name is not excluded.
A fresh host bridge check-in controls immediate delivery; offline wakes stay
queued. Run one bridge per enrolled host and register every active, non-excluded
derived agent on that host with bridge add.

  anx host enroll
  anx bridge install
  anx bridge add --as <name>
  anx bridge start
  anx bridge doctor

The bridge verifies its configured roster before check-in. It reads each
agent's notifications with a short-lived token from anx host token --as.
Wake claim, completion, and failure use host-signed CLI requests. With
agentctl present, wakes are launched via agentctl run and subscribed to
anx runs ingest so executions appear in the runs roster.
`
}
