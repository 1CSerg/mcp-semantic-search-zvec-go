@app.route('/api')
@require_auth
def handler(req):
    class Inner:
        pass
    return "ok"

class DataModel:
    def __init__(self):
        self.val = 1
