__all__ = ["__version__"]

try:
    from importlib.metadata import PackageNotFoundError, version as _distribution_version

    try:
        __version__ = _distribution_version("anx-agent-bridge")
    except PackageNotFoundError:
        __version__ = "0.0.0+dev"
except Exception:
    __version__ = "0.0.0+dev"
