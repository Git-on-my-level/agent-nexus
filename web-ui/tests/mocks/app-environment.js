export const browser = false;

let _dev = true;

export function __resetDev() {
  _dev = true;
}

export function __setDev(value) {
  _dev = value;
}

export { _dev as dev };
