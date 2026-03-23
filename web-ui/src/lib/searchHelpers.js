import { coreClient } from "./coreClient.js";

export async function searchThreads(query, limit = 20) {
  const response = await coreClient.listThreads({
    q: query,
    limit,
  });
  return response.threads || [];
}

export async function searchDocuments(query, limit = 20) {
  const response = await coreClient.listDocuments({
    q: query,
    limit,
  });
  return response.documents || [];
}

export async function searchActors(query, limit = 20) {
  const response = await coreClient.listActors({
    q: query,
    limit,
  });
  return response.actors || [];
}

export async function searchBoards(query, limit = 20) {
  const response = await coreClient.listBoards({
    q: query,
    limit,
  });
  return response.boards || [];
}

export async function searchArtifacts(query, limit = 20) {
  const response = await coreClient.listArtifacts({
    q: query,
    limit,
  });
  return response.artifacts || [];
}
