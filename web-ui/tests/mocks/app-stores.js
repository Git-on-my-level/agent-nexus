export const page = {
  subscribe: (fn) => {
    fn({ url: new URL("http://localhost/"), params: {} });
    return () => {};
  },
};
export const updated = {
  subscribe: (fn) => {
    fn(false);
    return () => {};
  },
  check: async () => false,
};
