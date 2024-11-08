# Reputation on Tatanka Mesh

- Reputation is used to gain access and privilege on Mesh, with higher reputation
associated with more privilege
- Clients and Mesh nodes maintain separate but connected reputation systems
- Clients track reputation for every other client (counterparty/peer) that they
interact with. Each counterparty is assigned a score based on their interactions.
Scores are reported to the Mesh when they change
- Mesh nodes aggregate reputation tuples (scorer, scored), and derive a meta-score
for each client based on the aggregated data
- The Mesh node's meta-score is available upon request, and may be broadcast or
attached to transmitted communications in certain contexts
- How reputation corresponds to privilege is determined by the P2P (client-only)
protocol. An exception may be that simple access to the network can be denied
for clients whose meta-score is too low

## Counterparty Scoring

- The score that a client might assign a counterparty is limited to the range
± 128
- Some protocol-derived number of time-sequenced counterparty interactions is
tracked in order to derive a score
- A single positive interaction with a client might generate a +1 difference
to the counterparty's score
- A single negative interaction with a client might generate a much larger negative
difference to the client's score
- Every time the score for a counterparty changes, it is reported to Mesh

## Bonding

- A bond is some amount of value that is time-locked and is inaccessible
to the bonder for some period of time.
- Bonding in Mesh serves 2 purposes
  1) Give access to Mesh
  2) Increase privilege





