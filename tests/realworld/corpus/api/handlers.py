"""HTTP handlers for the realworld corpus."""

from flask import jsonify, request


def handle_health():
    """REALWORLD_PY_HANDLER health check endpoint."""
    return jsonify({"status": "ok"})


def handle_search():
    query = request.args.get("q", "")
    return jsonify({"query": query, "results": []})
