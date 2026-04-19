export const page = {
  subscribe: (fn) => {
    fn({ url: new URL("http://localhost/"), params: {} });
    return () => {};
  },
};
