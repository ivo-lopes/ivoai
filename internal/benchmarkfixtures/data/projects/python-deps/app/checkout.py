from pricing.tax import tax
from pricing.totals import total

def checkout(amount: int) -> int:
    return total(amount)
