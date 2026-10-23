export function save() {
  return fetch("/cards/card-one", { method: "PATCH", body: "{}" });
}
