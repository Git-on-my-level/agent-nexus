export function save(client: {
  updateBoardCard: (id: string, body: object) => void;
}) {
  client.updateBoardCard("card-one", {});
}
