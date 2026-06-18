from functools import wraps


def require_auth(handler):
    @wraps(handler)
    def wrapper(request, *args, **kwargs):
        if not request.headers.get("Authorization"):
            raise PermissionError("missing token")
        return handler(request, *args, **kwargs)

    return wrapper


class UserHandler:
    @require_auth
    def get_profile(self, request):
        return {"user": request.user_id}
